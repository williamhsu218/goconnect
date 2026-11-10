# GoConnect 1.0.16 发布与验收记录（2026-11-10）

**已安装 build 76、升级网络服务并完成名单与全局模式验收；GitHub stable/latest、四附件与固定签名更新源均已公开回验。原名单主线已恢复，公司副连接保持关闭。沿用用户确认的 IPv4/TCP 发布范围。**

## 源码、构建与信任链

- 以最新默认分支 `3b1e85f59abafb967c59a21cc2145836c6f9282d` 为基础，集成实现提交 `800a70e70869e6b427406a3ad632f1e88b911b58`。冻结构建源码为 `1c015dcf35f2b872ee0326e0d597b4feb355a2cd`，annotated tag 对象 `0d4fc0987aa94f36d23037347b122ce79e67f8aa`。tag 指向构建与验收提交，后续发布记录单独提交，不移动 tag。
- 原开发分支、索引及已有修改保留，使用项目外 worktree；仅显式同步本版发布元数据与记录。未把用户配置、原始日志或私有验收材料放入公开源码。
- 团队 RX44JC39GB，Apple Development + Hardened Runtime，既有 Bundle ID、Ed25519 账户/公钥和固定 feed 不变。不是 Developer ID 分发签名，未经过 Apple 公证；Apple Silicon / macOS 26.0 及以上。
- 完整 `--verify --team-sign --export` exit 0：47 项 Python、221 项 Swift（0 失败）、4 个 Go 包 race、补丁内核规则/进程边界、包内 Live/集成、动态依赖及严格嵌套签名检查通过。权限/环境相关跳过项不计为通过，实际安装验收另列。
- 固定内核 `v1.19.30-goconnect2`，上游 SHA-256 `ee8a7107707e4bd485460139b1944e7be30016393783f2b4e928c14880c8ca8b`，补丁 SHA-256 `df40af021f4d2e65f52ac0dd06257463f4f5fb552ea5d588d376f4a3ea437506`。源码附件 138 条记录、122 个实际链接 Go 模块，所有 manifest、归档 payload 哈希与二进制 h1 均核验。
- 沿用官方 Sparkle 2.10.1 SDK，SPM ZIP 10197413 字节，SHA-256 `4c2e48a3e50ebcc2c88a58a4c7264a1a99c3c31857ce754357afb9cdb323022a`。第三方 GnuTLS 来源初次下载超时，经现有主线补齐并通过原始 SHA-256 后重新完成收集，未更换来源内容。

## 功能与原生安装验收

- 已授权窗口内正常断开、退出、备份、安装，再由诊断页正常升级网络服务和重连。安装 166 个文件、9 个符号链接与冻结候选完全一致；实际运行 helper、worker、内核映像与候选 Runtime 哈希相同。
- 用同团队签名的临时 App 启动包外测试可执行文件，仅临时加入父 App，子文件没有独立加入名单。名单模式下父 App 子进程的公网出口与 VPN 一致，同文件由独立命令启动则为原网络出口；两者不同。
- 全局模式下将同一父 App 临时列为直连排除：父 App 子进程保持原网络出口，而同一个独立程序遵循全局 VPN 出口。结果证明继承关联没有扩大到同文件的无关启动实例。
- 上述代理覆盖及直连排除与此前隔离真实进程 TCP/UDP 边界回归合并评估；独立 CLI 的固定入口、显式 CLI 规则、共享解释器/其他 App/未知签名边界保留。未使用真实 Claude 登录会话或付费模型任务冒充功能验收。
- 名单与全局分别 20 组系统 DNS UDP/TCP A/AAAA 检查通过，共 40 组。名单公网 fake-IP 不返回虚拟 ULA AAAA；全局保留真实答案。AI API HTTPS 证书校验通过，未认证 GET 返回 401；此为传输检查，不代表登录后完整 AI 功能或任意客户端零泄漏。
- 国内站点、LAN 网页、名单模式下公司域名 curl 访问正常。额外 Go HTTP/2 客户端对公司站点曾返回 EOF，而系统 curl 返回 200；本轮未修改该公司服务，未将该项写成公司副线实测通过。
- Chrome 实际打开公网登录页，名单与全局均渲染，未观察到本地网络授权提示，控制台无 error/warn；原站点权限与浏览器保护不变。此为指定页面检查，没有重复宣称全站 DNS 泄漏检测或全部浏览器业务已通过。
- 无公网 IPv6 的物理环境中，TUN 的预期 IPv6 地址及 IPv4/IPv6 两条半默认路由安装验证通过；正常断开后本轮 TUN 路由和回环默认 DNS 清理通过。所选 VPS 只有 IPv4 出口，IPv6-only 端到端仍不属于已验收能力。
- 原生更新结果弹窗监测 60.507 秒：同一 7 个生产进程与租约身份，54 次续约，最大租约年龄 1.029 秒，6 次真实代理请求均为 200。工具无法定位后台弹窗时由用户点“好”关闭，之后观察到主窗口速率继续更新，同一连接仍可请求。
- 结束时恢复原名单配置：8 个代理目标、15 个直连 App、9 条直连域名/IP。configuration、subscriptions、company-connection 三个 JSON 语义与原始备份一致，后两者字节未改；测试 App 从配置和 Applications 移除，流量历史等用户数据不回滚。公司副线按用户要求在公司 LAN 保持关闭，没有重新认证公司服务器。

## 隔离真实 OTA

从实际已安装旧 build 75 派生独立 QA，真实下载、验签、安装并自动重启至 76；监测确认自动重启且连续 12 个样本为单实例。安装后 166 个文件、9 个链接与 QA 目标完全相同，严格签名通过，生产候选字节未改。

篡改 XML 被原生客户端拒绝，ZIP GET 为 0；合法签名下载完成后，未保存草稿阻止安装。清除草稿并明确确认后才正常结束模拟连接、交接安装。新版本启动与自动检查冷启动、无更新结果均无残留连接门禁，QA 偏好已恢复。真实主 VPN 保护另由上述原生模态监测证明。

## 冻结附件与公开回验

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.16-macos-arm64.zip` | 32336237 | `613604fbd6fe76842809db2bf4d4f57abd088bca363471464b34f7762251dc86` |
| `GoConnect-1.0.16-third-party-sources.tar.gz` | 216716427 | `f9765f68c5e3a63f27a9c2be2b05bff7331433a05c5039663f7e04acccfacf13` |
| `appcast.xml` | 1549 | `66441f21deb28b749b18968aed488919911f754a3def820c06cc2c3686ef0254` |
| `SHA256SUMS` | 287 | `de7c76d74c2bcaac81d4069c02e342e2867d9823c1d55ec9db9aad84924e58bb` |

四附件先上传 draft 并核对 tag、源码提交、uploaded 状态、大小及 GitHub 全包 digest，再发布 [v1.0.16](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.16)（Release ID 440157809），stable/latest 均正确。公开应用 ZIP、XML 与 SHA256SUMS 匿名完整下载、逐字节和哈希比对，官方 SDK 验证完整 XML 与 ZIP 的 Ed25519 签名及 enclosure 元数据。

第三方源码采用本地完整来源审计、GitHub 全包 digest 和匿名 128 KiB Range 逐字节公开比对，**没有重新完整下载源码附件**。上传及匿名回验沿用现有主线上的任务专用 loopback 中转，不修改系统代理、DNS 或 VPN；认证只在进程内处理。

## 固定生产更新源

公开附件验收后将冻结签名 XML fast-forward 推送至 updates：`d367143ba70fd37102bc19b6d96f862b1201dd2e`，父提交 `a369dbe37d876f49b1bac918912a61de20e61b9b`。推送前重新验证旧 feed 签名及 build 75，防止覆盖并发新版本；不 force push，不覆盖公开 tag 或附件。

固定 [客户端更新地址](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml) 经匿名 curl 与原生 URLSession 读回，均与冻结 1549 字节一致，SHA-256 `66441f21deb28b749b18968aed488919911f754a3def820c06cc2c3686ef0254`；签名、stable、1.0.16/76、enclosure URL/长度/签名与最低系统正确。旧正式 build 75 能识别更高 build，既有公钥与固定 URL 兼容。

固定 URL 在任务中转出口曾命中上一版 build 75 的缓存，原始对照保留。当前原生网络的匿名 curl 与 URLSession 均读回 build 76 并验签通过；未修改 URL、查询参数、系统代理或 VPN 来绕开验收。README 的下载附件名在本次记录提交中同步更正为 1.0.16，构建 tag 和公开附件保持原字节。

## 保留、边界与证据

- current **1.0.16/76**，紧邻 rollback **1.0.15/75**，每目录仅一个 ZIP 和 SHA256SUMS；原安装替换前备份在回滚包核验后清理。
- IPv4/TCP 按用户既有授权放行；所选公司网络/VPS 不具备 IPv6 出口，未称 IPv6-only 端到端通过。公司副线本轮关闭，未称完成新公司隧道验收。
- AnyConnect UDP 历史剩余偶发超时仍为已接受的非阻断限制，未重做稳定性长测，本轮回环 UDP 回归不代表全部修复。
- 包外继承需可验证的活跃同用户 App 启动链及有效签名。共享解释器、脱离父链、其他 App 边界、未知进程、应用自带 DoH/独立 DNS 等保留边界，不能保证所有外部客户端零泄漏。实现见 [App 子进程覆盖](APP_CHILD_ROUTING_2026-11-10.md)。
- 原始私有证据：`~/.codex/visualizations/2026/11/10/goconnect-release-1.0.16/`；保存签名构建、138 项来源审计、OTA、实际安装/模式/租约、draft/public/fixed-feed、保留与清理回执。含配置的材料不上传 GitHub。任务专用测试 App、QA、SDK、下载和 worktree 清理时保护实际运行路径与全部用户数据。
