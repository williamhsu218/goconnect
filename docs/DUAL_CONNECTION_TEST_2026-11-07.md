# 双 VPN 第一轮隔离测试构建

日期：2026-11-07。基线：默认分支 `5837d903321e2d461540553102586ff0132e63ce`，已发布 1.0.8 / build 68 之后的验收记录提交。工作分支 `codex/dual-vpn-test-20261107`；未更新正式语义版本、发布 tag、更新源或分发包保留记录。

## 本轮交付范围

- 单一透明路由控制器，预留稳定的公司 SOCKS 与 UDP/TCP DNS 入口，初始及断副后均为拒绝态；prepare/commit/block 按 generation/revision 执行，撤销只关闭公司所属流。
- HY2 主连接 + AnyConnect 公司副连接的独立生命周期、密码、取消、故障隔离、统计和更新保护；副错误不调用主连接停止。公司设置草稿及主副使用中配置均受保护。
- 独立 `company-connection.json` sidecar（0600），主配置保持 schema 7。默认不启用、不自动连接、不复用主令牌。
- 公司网段只接受明确 RFC1918 IPv4，域名使用明确后缀；拒绝 LAN、Tailscale、直连、原生私有 DNS 与隧道地址冲突。服务端 split 只作为更窄的许可上限，排除网段、受限 include 和缺失可用 include 不扩大授权。
- 公司 DNS 默认从已认证副 VPN 获取；手动 DNS 可覆盖。通过身份认证的 Unix 通道以 TCP 53 访问，只为 DNS 服务单独授权；CNAME 及最终 IPv4 仍须属于批准范围，业务连接始终使用验证后的 IP，禁止二次 UDP DNS。IPv6 公司业务及 UDP 业务暂不支持，AAAA 返回 NODATA。
- 公司 DNS 作用域随主控制器预留，副启停不改默认 DNS、TUN、内核配置或监听端口。仅全退出时撤销本控制器 DNS lease、路由与核心；此原生作用域路径本轮未在系统中启用。
- AnyConnect 外层强制 TCP，经 root 所有的受限 CONNECT 入口，每次校验完整目标、端口与令牌，再以物理接口绑定拨号。未知重定向不能先走主 VPN。`VPNGATEWAY` 在 HTTP 代理模式可能是 127.0.0.1，实际目标授权以受限代理为准。
- 后端 Unix socket 每次建连校验0600文件、UID、内核 PID/start-time 及精确 helper CDHash。稳定端口、SOCKS 密码或一次端口探测不能替代服务端身份。

## 测试包隔离

构建入口：

```sh
./script/build_and_run.sh --verify --isolated-test --team-sign --export <项目外的新目录>
```

测试包使用 `com.willhsu.GoConnect.DualVPNTest`，应用名 `GoConnect Dual VPN Test.app`。团队签名沿用 RX44JC39GB。编译时停用系统连接、正式服务安装/移除、正式日志读取和应用更新，不创建菜单栏副本；数据与钥匙串使用测试命名空间。首次仅写入标明“测试示例”的合成配置，不读取正式用户配置。测试包用于界面/配置验收；不是已经可以在当前正式 VPN 上接入公司的网络版。

运行网络功能的源码已集成，但在现有正式连接不能中断的约束下，尚未替换 helper 或执行真实 TUN 双连接。后续需单独安排公司连接元数据、本机认证及网络验收窗口，再提供可接管系统的候选。

## 协作与纠偏

| 参与方 | 请求参数 | 实际工作与回执 |
| --- | --- | --- |
| Claude Code | claude-opus-5-5 / high | 独立 CompanyConnectionStore 与生命周期测试；平台 modelUsage.canonicalModel 匹配，effort 未回执 |
| Agy | gemini-4-argon-medium / medium | internal/company 的策略、TCP DNS、CNAME、异常报文及单元测试；run_agy SUCCESS，MCP 未返回实际 model/effort 字段，不能声称型号/强度均已独立核实 |
| Codex | 当前会话及一个子 agent | 共享 schema、IPC、特权边界、网关出口、状态集成、原生 UI、签名构建及验收；完整复核外部改动并修正 |

复审修复：不依赖公司域名根存在 A 记录；DNS 解析后重新核对取消 generation；保留服务端排除与受限 split；os.Executable 出错时禁止绕过 CDHash；独立公司随机令牌；延迟阻断回执按最新 revision 重发；公司设置草稿纳入更新保护。实际 OpenConnect 验证还补齐了代理 407 challenge 和仅对本机随机令牌的 `--proxy-auth=basic`。

## 验收口径

代码构建提交：`b8e60f57fc9ae2185f65c22a01e2776f1a8277c4`。完整检查提交 `225f71e` 之后只修改了一个原生表单文件的标签、占位与输入框布局；最终包中的 transport 和 Mihomo CDHash 均与完整检查包一致。

| 门禁 | 本轮结果 |
| --- | --- |
| Swift | 完整 169 项通过，含公司生命周期、独立速率、旧配置 sidecar 兼容及更新草稿保护 |
| Go | 全套 `go test -race -timeout 60s ./...` 通过；公司策略、DNS 异常/CNAME、身份、陈旧 revision、拒绝态、定向 drain 与 split 限制测试通过 |
| 真实 Mihomo 回环 | 3 次 attach/block/reconnect，持续主 TCP 保持；稳定数据/DNS listener 不换端口；公司文字 IP 携带域外 Host 仍经公司出口。没有创建系统 TUN 或 DNS lease |
| 真实 OpenConnect 回环 | 受限 CONNECT 代理认证与证书验证通过；公司角色 CSTP TCP、Unix socket/PID/start-time ready、UDP ASSOCIATE 拒绝及 EOF 清理通过；原有协议回归通过 |
| 原生界面 | 最终 ZIP 解压运行；公司线路/网段/域名/DNS 字段可编辑且具有 AX 标签；默认路由输入被拒绝；手动 DNS 保存回读、清空恢复自动模式、重启后配置保持通过；网络接管禁用提示通过 |
| 签名及隔离 | 实际 Code Signature 的 TeamIdentifier=RX44JC39GB、Hardened Runtime、deep/strict 校验通过；独立 bundle ID，更新关闭；sidecar 0600 |
| 正式环境保护 | 正式 App 二进制、configuration.json、subscriptions.json 的 SHA-256 前后一致；原正式 App/helper/Mihomo PID 保持；服务仍 ready+busy；没有安装或发布 |
| 清理 | Swift scratch、临时 Runtime、两轮临时 UI App/数据、worker worktree 已移除；旧 UI 候选与已知生成缓存移入废纸篓。正式/回滚分发包未变 |

最终交付：`/Users/willhsu/Downloads/GoConnect-DualVPN-Test-20261107/GoConnect-DualVPN-Test-1.0.8-macos-arm64.zip`，31,990,549 字节。SHA-256：`61f01a684247886e57c92558aaefdb3fda511b3115ec0624c7ebdb1588ca65a3`，同目录 SHA256SUMS 已逐字核对。ZIP 内只有独立命名的 `GoConnect Dual VPN Test.app`，没有保留解压副本到项目。

完整 G0 仍未放行：已证明的是回环隔离子项，实际物理接口绑定、真实系统 TUN/DNS 和 Finder SMB 属于下方待验收范围。此包的系统连接入口在编译期停用，不应当作已可使用的真实双 VPN 版本。

原始证据目录：`/Users/willhsu/.codex/visualizations/2026/11/07/goconnect-dual-vpn-build/`，不提交私有原始日志。

以下均保持待验收：真实物理接口上的公司网关、公司认证/MFA兼容、真实 macOS supplemental resolver 选择及缓存、系统 TUN 入口、内网页面/SSH、Finder SMB 挂载和文件读取、真实双连接故障/睡眠/崩溃与 OTA。回环协议与注入边界测试不能替代上述实网门禁。既有 AnyConnect UDP 问题未修复、未重新验收。

依据：[Mihomo 嗅探配置](https://wiki.metacubex.one/config/sniff/)、[OpenConnect 官方代理拨号实现](https://github.com/openconnect/openconnect/blob/master/ssl.c)。本轮通过真实内核回环转发确认 skip-dst-address 与公司域名跳过嗅探策略；物理接口绑定仍未作实网证明。
