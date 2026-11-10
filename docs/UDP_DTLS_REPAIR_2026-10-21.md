# 2026-10-21 UDP 复验与 TLS/CSTP 对照

**最终状态：TLS-only 方案复验失败，已从源码、已安装 App 和实际服务中撤回。当前恢复 0.11.15 / build 53，真实连接已恢复；UDP 问题未解决，不正式放行。** 已验收分发包保持 0.11.14 / 0.11.13。

用户要求“重新验收修复后再放行”。不接受 build 53 的 UDP 失败作为放行豁免；原始 46/52 VPN UDP、15/16 直连对照和全部失败事务保持不变。见 [安装验收](CANDIDATE_INSTALL_0.11.15.md)。

## 已核实的传输差异

当前 OpenConnect 为包内 v9.21 / GnuTLS 3.8.13。只读 socket 与 nettop 检查确认，build 53 同时持有到 VPN 网关的 TCP 和 UDP socket，实际数据主要经过 UDP：15:42:04–05 的采样中 TCP 收发字节保持 8854/5296，UDP 收发继续增加。记录仅保存协议、网卡、字节与状态，不保留网关地址。

因此当前会话实际采用 DTLS 数据通道；不能以过去 TCP-only 会话的验收结果覆盖本次 DTLS 路径。两个不同 STUN 服务的 VPN 请求在 15:32:37、15:33:07 同时出现无回复，而其后的新事务恢复。写入本地通道成功不能证明 OpenConnect 已向网关发出，更不能定位丢包的具体设备。

## 0.11.15 / build 54 对照候选

为用户态 AnyConnect 桥接选择 `--no-dtls`，使内层 TCP/UDP 数据均经过证书校验的 TLS/CSTP 通道。保留原始 UDP 报文内容、单次发送、15 秒探针期限、认证失败终止、30 秒健康/租约窗口、60 秒原生重试及 75 秒总恢复保护。分流名单、线路配置和密码保存设置不变。

OpenConnect 官方提供该选项以关闭外层 DTLS；正常网络下 DTLS 有性能优势，因此 TLS-only 可能牺牲部分吞吐或产生 TCP 队头阻塞。此处作为待复验的稳定性方案，不能仅凭参数改变认定修复成功。参考：[官方手册](https://www.infradead.org/openconnect/manual.html)、[v9.21 CSTP 握手实现](https://gitlab.com/openconnect/openconnect/-/blob/v9.21/cstp.c)。

新增原生集成门禁：合成 TLS 网关检查 CONNECT 握手不再提供 DTLS 协商，同时验证内层 TCP/UDP 内容、证书认证和退出。旧候选先复现失败，日志 `tls-only-before.log`；修复后须对实际打包组件重新通过。测试仅检查协商是否出现，不记录 DTLS secret 或其他响应正文。

新增固定诊断 `openconnect.transport_tls_only=1` 表示客户端启动策略；实际通道须通过 socket/nettop 和端到端探针独立核实。

## 放行条件

- 完整候选验证、安装/服务文件与签名一致性、用户数据保护通过。
- 实际特权服务/TUN 门禁通过，清理正常。
- 真实网关确认数据走 TCP/TLS，既有透明分流与 SOCKS 路径重新执行 HTTPS/UDP 测试；全部失败原样保留，不重发以掩盖失败。
- 复验通过后才将最终 0.11.15 与已验收 0.11.14 归档。若 TLS-only 仍出现同类问题，撤回该方案并继续定位，不放行。

证据目录：`~/Library/Logs/GoConnect/Incidents/2026-10-21-install-0.11.15-1515/`。

## build 54 实际复验结果

完整候选验证通过：76 项 Swift 测试、Go race、release 构建与打包、签名与动态依赖、内置 OpenConnect TLS-only 协商及 TCP/UDP 集成测试。隔离集成套件 115.368 秒；75 秒恢复硬保护与调度暂停恢复分别耗时 76.08、8.61 秒。已安装服务/TUN 的公司网段、whitelist/global 两模式测试全部通过，27.614 秒。上述成功不能替代真实网关验收。

15:48 起对真实网关观察 180 秒。OpenConnect PID 72964 只持有 TCP socket；nettop 的 TCP 收发字节持续增长，确认实际采用 TLS/CSTP，而不是仅从启动参数推断。通过原有白名单中 ChatGPT 内置 Node 产生透明 VPN 流量，另用认证 SOCKS 并行对照，没有改用户名单、探针报文内容或原有超时期限。

| 路径 | HTTPS 成功/总数 | UDP 成功/总数 |
| --- | --- | --- |
| 透明 VPN | 90/90 | 33/34 |
| SOCKS VPN | 90/90 | 27/30 |

两路 HTTPS 出口摘要一致；89 次会话采样全部 ready、VPN 可见，最大租约年龄 0.999 秒。UDP 单次发送，15 秒超时；失败等待占用发起窗口，因此不能把计划频率计算出的 36 次冒充实际事务数。

同时对两个 STUN 服务发起 8 轮新事务，并以绑定物理网卡 en8 的直连作对照：

| 端点 | 物理直连 UDP | SOCKS VPN UDP |
| --- | --- | --- |
| Cloudflare | 8/8 | 7/8 |
| Google | 7/8 | 8/8 |

本轮真实 VPN HTTPS **180/180**，UDP 合计 **75/80**，物理直连 UDP **15/16**。80 个 VPN UDP 事务中，75 个有完整九阶段记录；5 个失败事务都完成 `pipe_write`，未观察到 `pipe_read` 及回复阶段。桥接与 App 诊断丢弃计数均为零。原始失败未重试改判，也未通过增加期限或改探针隐藏失败。

这反驳了“关闭 DTLS 即可解决本次 UDP 丢失”的方案；无法以此排除 OpenConnect 内部、本地 Unix 通道、VPN 网关、外部链路或 STUN 服务中的全部可能。`pipe_write` 成功只代表本地 socket 写入成功，不证明网关或端点收到报文。不同端点的直连也出现失败，仍不能由此把 VPN 的失败都归因于外部网络。

## 撤回与尚未完成的工作

用户要求修复复验后再放行，并确认没有网关侧日志或抓包权限。已撤回 `--no-dtls`、其专用策略诊断及仅服务于该策略的测试断言，源码版本恢复 build 53。已安装 App 与特权服务同步恢复到原 build 53，通过版本、签名、76 个应用文件、17 个服务文件逐项校验；用户重新认证后正式界面已连接，服务 ready / busy。56 个用户数据文件和原 0.11.14 特权组件均与升级前哈希一致。

客户端错误原因优先级、重新认证提示、Darwin 路由缩写解析修复保留。0.11.15 仍是已安装的未放行候选；未调用 `release_artifacts.py retain`，未发布 GitHub Release，未把 0.11.15 设为已验收正式包。

后续需要新的定位能力，而不是反复运行同一探针直到偶然全绿：可控远端 UDP 回显端点与收发记录，或 OpenConnect 收发边界的最小脱敏诊断，再与网关/远端记录对齐。当前缺少这段证据，不能声称 UDP 已修复，也不应继续调整缓冲、重放不确定报文或放宽门禁。

主要证据：`build54-verify.log`、`build54-installed-tun.log`、`tls-transport-after.json`、`tls-transparent-live.jsonl`、`tls-socks-live.jsonl`、`tls-udp-compare.jsonl`、`tls-bridge-live.jsonl`、`tls-live-summary.json`、`restored-build53-readback.json`。build 53 的失败证据另保留，不能被本轮结果覆盖。

撤回后已核对运行进程无引用，将无效 build 54 的项目外候选目录和暂存 App 移入废纸篓，可恢复，记录为 `build54-cleanup.json`。当前 build 53 候选、原安装回滚材料、私有失败证据及特权服务组件保留；项目清理预检清单为空。
