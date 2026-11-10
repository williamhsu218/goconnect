# GoConnect 1.0.15 发布与验收记录（2026-11-10）

**已按用户授权的 IPv4/TCP 范围完成发布：GitHub stable/latest、四附件、完整 feed 与 ZIP 签名及固定生产更新地址已公开回验。本机安装 build 75，原名单代理主线已恢复，公司副连接保持关闭。**

## 源码、构建与信任链

- 从核实为最新正式版本的默认分支 `ec09e78e9e413d5adec4e53f9cbfc1a761eeb600` 建立隔离发布分支，集成候选提交与既有未提交修复；原开发分支、索引与运行代码修改保留。公开构建提交 `6d2557c3e5874b0142270a4b5a4eea082cd13bed`；annotated tag 对象 `24b75821df70c5baf34c0ab8469dbdde6e4c8723`。tag 固定构建提交，后续发布记录另提交，不移动 tag。
- 团队 RX44JC39GB，Apple Development + Hardened Runtime，既有生产 Bundle ID、Ed25519 账户/公钥和固定 feed；不是 Developer ID 分发签名，未经过 Apple 公证。Apple Silicon / macOS 26.0 及以上。
- 更新启用、stable、`SURequireSignedFeed`、`SUVerifyUpdateBeforeExtraction`、用户确认下载/安装与自动检查默认关闭均保持；未放宽 helper 精确 CDHash、授权或退出保护。
- 团队签名 `--verify --team-sign --export` exit 0：45 项 Python、219 项 Swift（0 失败）、4 个 Go 包 race、补丁内核边界、配置、包内 Live/集成、动态依赖和深度严格签名检查通过。受权限/环境限制的原生查询、安装/TUN 专用测试跳过项不计通过，实际安装验收另列。
- 内核固定 `v1.19.30-goconnect1`，上游 SHA-256 `ee8a7107707e4bd485460139b1944e7be30016393783f2b4e928c14880c8ca8b`，补丁 SHA-256 `f277fead7f42e9ce93c00d388dd8b66998f5a336547335978fed2f32fbb3aa1f`。来源附件本地完整审计 138 条记录、122 个实际链接 Go 模块及全部 manifest/归档 payload 哈希。
- 沿用已验证的官方 Sparkle 2.10.1 SDK，SPM ZIP 10197413 字节，SHA-256 `4c2e48a3e50ebcc2c88a58a4c7264a1a99c3c31857ce754357afb9cdb323022a`。

## 安装与原生验收

- 已授权窗口内正常断开、退出、备份、安装并升级网络服务，然后重连与切换模式。安装 175 个普通文件/符号链接与冻结 ZIP 完全一致，服务、worker、Mihomo 映像哈希与包内一致；原应用保留至正式回滚包核验完成。
- 名单与全局各 16 组系统 DNS UDP/TCP A/AAAA 检查通过；名单内/外与全局默认/排除原生应用的实际出口分别验证。名单公网映射不返回虚拟 ULA AAAA，全局保留实际解析；直连国内与公司 LAN 路径正常。
- 两家 AI API 的 TLS 证书检查通过，未登录请求返回 401；国内站点与 LAN 网页返回 200。此结果证明测试请求的 DNS/传输/证书路径，不代表登录后全部 AI 业务或任意客户端零泄漏。
- Chrome 156 保持原站点权限：未列入代理名单时使用原网络 DNS，符合名单语义；全局模式 DNS 检测显示 Google、未检测到泄露。未关闭本地网络保护或批量授予站点权限。
- 物理网络无可用公网 IPv6，TUN 的 `fdfe:dcba:9875::1/126` 地址及两条 IPv6 精确半默认路由安装通过；正常断开后本轮路由与默认回环 DNS 撤销。Tailscale 保留。
- 原生更新结果模态弹窗保持 60.014 秒：同一 7 个生产进程及 lease 身份，60 次续约，最大 lease 年龄 1.05 秒，6 次代理请求均为 200。关闭后仍正常。
- 直连地址导入在实际原生窗口预览后取消，原有 9 条规则不变；测试应用/规则移除。
- 验收结束恢复原名单线路；configuration、company-connection、subscriptions 三个 JSON 与安装前字节完全一致。公司功能原配置保留且关闭，其他流量历史等用户数据不回滚。发布阶段再次核对 175 项安装清单、7 个生产进程、配置哈希与代理请求，均未改变。

## 隔离真实 OTA 与保护

独立 `com.willhsu.GoConnect.UpdateQA` 从实际旧版 74 下载、验签、安装并自动重启到 75（PID 62835 → 63580），连续 12 个 0.2 秒样本保持单实例；166 个文件、9 个符号链接与 QA 目标一致，签名通过。生产 ZIP 没有被 QA 改写，两份 QA 的可执行文本与对应生产源一致。

篡改 XML 被实际客户端拒绝，拒绝时 ZIP GET 为 0。合法下载期间模拟连接与未保存草稿保持；草稿阻止最终安装，清除后明确确认才正常结束模拟连接并交接。新 build 可连接，自动检查冷启动/无更新/失败或取消不留连接锁，QA 偏好恢复。生产连接保护另由上述真实模态验收证明，未把模拟状态当成主 VPN 实测。

## 冻结附件与公开回验

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.15-macos-arm64.zip` | 32314561 | `321a7fd495981d9e399f8bb7c4ab7845db25225ecf9039fa593cdac07e15fb72` |
| `GoConnect-1.0.15-third-party-sources.tar.gz` | 216710617 | `f14f813a15e0c9e6c20046559024dec61eef36cb41c322b8e15ce88eb10cfa1c` |
| `appcast.xml` | 1540 | `b2e1c01e9902483fc1b62ded840c7cc2493a41861d0dfd82ead46bd3449e226d` |
| `SHA256SUMS` | 287 | `73700cff4a43c29de9082a53709a96b4837989e2985bbff3e44d57fc4eaffccb` |

四个附件先以 draft 上传，核对 tag 源码、uploaded 状态、尺寸与 GitHub 全包 digest 后，公开 [v1.0.15](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.15)（Release ID 440013346），stable/latest 正确。应用 ZIP、XML、SHA256SUMS 匿名完整下载并逐字节/哈希比对；官方 SDK 验证 XML 与 ZIP 的 Ed25519 签名及 enclosure URL/长度/build/最低系统。第三方源码公开回验采用 GitHub 全包 digest 与匿名 128 KiB Range 区段比对，**未重新完整下载**；本地已完成全包来源审计。

首次大附件中转上传较慢，取消本次未完成上传后重新读回 draft，仅补传缺失来源附件；已上传且哈希正确的附件没有覆盖。直接 GitHub 上传域名发生 TLS EOF，使用当前主线上的任务专用 loopback 中转完成，不更换系统代理、DNS 或 VPN。认证只在本机进程内使用，不写入公开记录。

## 固定生产更新源

公开附件回验后，将冻结签名 XML fast-forward 推至 updates：`a369dbe37d876f49b1bac918912a61de20e61b9b`，父提交 `3c99a94f35a6c8aa1ac5512a5f5fbac198cb3886`；推送前重新验签旧 build 74 并检查没有并发更高版本。

固定 [客户端更新地址](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml) 由匿名 curl 和原生 URLSession 读回，均与冻结的 1540 字节逐字节一致，SHA-256 `b2e1c01e9902483fc1b62ded840c7cc2493a41861d0dfd82ead46bd3449e226d`；签名、1.0.15/build 75、stable 及 enclosure 正确。旧版 74 的 build 更低，公钥与固定 URL 兼容。不改 URL、不覆盖 tag/附件、不 force push、不重建密钥。

## 放行范围、保留与证据

- **IPv4/TCP 限定放行**：IPv6-only 请求在新路径及保留域名的代理对照均 TLS 失败；用户确认公司网络和所选 VPS 均无 IPv6 出口，并明确授权按 IPv4/TCP 发布。TUN 地址/路由通过不等于 IPv6 端到端通过；客户端 IPv6 端到端仍待支持 IPv6 出口的环境另验。本次不是全部 IPv6 门禁通过。
- **公司副线未实链路复验**：用户明确要求在公司 LAN 暂不测试副 VPN，因此未进行公司服务器认证/重连。相关规则隔离回归覆盖不等于新的实际公司隧道验收；以往证据保留，当前副线关闭。
- AnyConnect 历史 UDP 部分缺陷已修复，剩余偶发超时已接受、发布非阻断；未重做该稳定性长测。公司业务仍限明确 IPv4/TCP。此次回环 UDP 出口隔离回归不表示历史 AnyConnect UDP 全部修复。
- 原始 AAAA-only 专用客户端、应用自带 DoH、独立 DNS、包外 helper、未知进程及未知公司域名继续遵循各自边界；不承诺任意客户端零泄漏。实现与协作记录见 [分流修复](ROUTING_MODE_FIX_2026-11-10.md)。
- retain 已核验 current **1.0.15/75**、rollback **1.0.14/74**，每目录仅一个 ZIP 和 SHA256SUMS。本次任务专用 QA、SDK、下载、临时应用和 worktree 按项目规则收尾，未清理活跃运行组件；最终清理回执与安装前独立配置备份保存在项目外。
- 私有证据：`~/.codex/visualizations/2026/11/10/goconnect-release-1.0.15/`，包括 team-verify、sources-audit、ota/summary、native/summary、modal-result、draft/public/fixed-feed-verification、publication guard、retention 与 cleanup 回执。包含用户配置的原始材料不上传 GitHub。
