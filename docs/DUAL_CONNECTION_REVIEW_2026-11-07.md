# 双 VPN：Agy 分块独立审查与 Codex 复核

日期：2026-11-07。源码基线：`5837d903321e2d461540553102586ff0132e63ce`。这是只读静态审查记录；更新后的设计见 [双连接方案](DUAL_CONNECTION_PLAN_2026-11-07.md)。没有产品实现、构建、安装、发布或真实双连接测试。

## 执行与证据范围

沿用用户指定的 `gemini-4-argon-medium` / `medium`，使用 Agy CLI 的 stream-json、plan 与 sandbox。MCP 的审查封装会省略初始化模型/目录回执，因此本轮直接保留同一 CLI 的原始事件；未改变全局工具设置、凭据或权限。

初审拆成六份互不依赖的固定源码片段，每份约 7.5–11.9 KB。每个任务都是新会话，只读其模块资料。两项过度推断各做一次有界补读更正，一项截断报告做一次有界补齐；Codex 另发现并增加一项固定依赖版本的 DNS 二次解析审查，共七个审查主题、十次调用，没有更换模型。

十次调用均返回 `SUCCESS`、退出码 0，`init.model=gemini-4-argon-medium`、`init.cwd` 与各工作副本匹配，未报告权限拒绝，副本未被 Agent 修改。这是 Agy CLI 自报回执，不是底层供应商的独立证明。medium 请求与型号后缀均已固定；平台没有独立返回最终 effort 字段。

终态成功只代表调用完成。Codex 还检查报告完整性、源码引用、调用链和方案范围；DNS 初稿虽返回成功却在字符串中截断，未作为完成结果使用。

## 七项复核结果

| 编号与主题 | 核实的源码事实 | 对方案的影响 |
| --- | --- | --- |
| R1 路由优先级与范围 | `transparent_routing.go:105–172`：本地/Tailscale先行，直连规则之后才是 `RemoteNetworks`，其目标仍为同一 `GoConnect`，最后是 App/MATCH。`route.go:75–82,266` 与 `transparent_routing.go:567,624–634` 已接入私网、数量、LAN/Tailscale重叠及主网关检查 | 新公司出口位于保留网络之后、普通直连/App之前；新增副地址族及主副网关检查，保留既有前置校验。不能声称已有校验缺失，也不需要第二个 OS VPN 网卡 |
| R2 外层握手与系统入口 | `main.go:177–210` 启动 OpenConnect；`main.go:425–430` 在 packetPipe ready 才回报网关。`transparent_session.go:234–259` 只固定当前 `r.Gateway`。`company_network.go:195–210,250–254` 区分核心内部转发与系统路由导入 | 公司拨号必须在认证前解决物理外层路径；未知重定向/新网关要在拨号边界受控。Finder/kernel SMB 仍需真实系统入口验收，不把旧独立 CompanyShared worker 并行启动 |
| D1 分域 DNS 与出口 | `managed_dns.go:85–105` 优先保留 resolver 自身接口；`151–197` 原生/直连域名和 DoH 分开处理。`main.go:430` 的 ready 包含 DNS，但 `AppStore.swift:441–455` 没消费该字段建立企业 DNS | 公司 resolver 元数据必须校验后接入稳定公司 DNS 入口，不能因 ready 存在字段就声称功能已有；不能只在字典末尾写一条规则就保证原生 DNS 优先级正确 |
| D2 DNS 租约与缓存 | `managed_dns_darwin.go:28–52,98–105` 用唯一临时键持有租约，恢复后回读；`managed_dns.go:252–295` 验证系统实际解析器；`transparent_session.go:117–138` 先撤销租约，再清路由/停 listener | 租约和 listener 归统一控制者，副开关保留它们；公司 DNS 只切换已认证 backend/拒绝态。源码没有证明按域精确清空系统缓存，需验证 TTL 和缓存影响 |
| H1 动态挂载与旧流 | `transparent_session.go:163–175,388–389` 启动时生成配置、运行时轮询；`route_command.go:24–38` 当前只接受 launch/probe；`RouteClient.swift:20–39` 没有第二个活跃控制会话入口 | 稳定入口与预留 scope 是待原型设计，不是现成热插能力。prepare/commit/block/detach、generation、定向关闭旧流、入口身份必须新增；不能保证完整 reload 无扰动或反向宣称所有 reload 必然重启 |
| H2 特权与恢复 | `service.go:277–278` 与 `app_capture.go:138–144` 单例；`session_recovery.go:58–94` 仅接受限定路由、最多16条；`201–212` 阻止在受管 core/接口仍存活时全局恢复 | 单 worker/lease 不放宽；第二物理网关单 IP 可复用旧记录。公司 CIDR→TUN 与单 IP 网关记录必须区分；副 detach 不调用全局 abandoned recovery |
| D3 SOCKS 域名二次解析 | `bridge/socks.go:106–112,168–185` 把域名交给 dialer；`bridge/stack.go:40–43` 进入固定 netstack。该版本 `tun.go:988` 调用 Lookup，`660–696` 先UDP且错误直接返回；`823–825` 文字IP直接返回 | 系统 DNS 仅用 TCP 仍不能消除业务域名 CONNECT 的 UDP 依赖。公司角色先受控 TCP 解析并验证结果，再使用文字IP拨号；不改变整个主连接的解析器或依赖库 |

D3 依赖为 `Transport/go.mod` 固定的 `golang.zx2c4.com/wireguard v0.0.0-20260522210424-ecfc5a8d5446`；只读取已存在的本地模块源码，没有下载、编译或改动依赖。引用行号属于上述固定提交/版本。

## Codex 的纠偏与裁决

1. **撤回“透明模式完全无校验”的判断。** R1 初稿只看构造函数，输入不足以证明整条调用链缺失。补充 `openControl → routeRequest.validate → transparentSession` 后，Agy 已撤回默认路由注入、LAN/Tailscale未检查等错误反例；真正新增的是副角色、地址族、双网关、专属出口和冲突/优先级。
2. **第二网关不等于新日志格式。** H2 已更正：合法物理接口与 gateway 的非 scoped `/32`、`/128` 满足旧格式（`session_recovery.go:84–92`），现有 pin 函数已使用它。新公司 CIDR→TUN 则不在旧捕获白名单；不能为了超过16条预算自动放宽校验。
3. **完成 DNS 截断报告，但不照收所有建议。** D1 补齐后，Codex 拒绝“SOCKS 只监听 TCP 所以没有 UDP”的推断：`bridge/socks.go:142–143` 实际有 UDP ASSOCIATE。首版暂不承诺 AnyConnect UDP，是验收范围决定，不是把已有分支误判为不存在。
4. **公司 CNAME 不能交给公网默认解析。** D1 给出的“跳出公司后缀后由默认规则接管”不符合防回落目标，未采纳。公司查询链始终使用公司 DNS；终端地址越界、循环或解析失败就拒绝。App 自带 DoH、未知文字IP和无法关联缓存有明确识别边界，不夸大成全局不可绕过的防漏。
5. **保留原生 resolver 接口和系统优先级边界。** `r.iface` 非空时不是强制走物理网卡。已有 supplemental/private resolver、直连域名与公司范围冲突先拒绝，不用字典覆盖顺序替代系统 DNS 验收。
6. **稳定端口不是服务端身份。** 即使知道 SOCKS 口令，假服务仍可接管旧端口并接受客户端；必须由存活控制者持有入口，并验证 Unix peer/受认证 backend 身份和 generation，而不止做端口健康检查。
7. **不全局改写 netstack。** D3 的 TCP 预解析设计收敛到公司角色的 resolver/dialer；拒绝通过开放 UDP 53 来绕开尚未验收的边界。保留业务 TLS/HTTP 字节，仅固定拨号目标为已批准文字 IP。
8. **避免过度肯定运行行为。** 未 pin 的副握手存在进入主 TUN 的风险，不把所有网络情况下的具体路径当作已抓包事实；完整配置 reload 的连接保留、系统 SMB 和新增路由恢复都是原型门槛。普通 HTTP、API返回204或多个模型同意均不代替真实验收。

## 收敛后的最小设计

- 单一 root worker/TUN、唯一 lease；主与公司两个独立用户态 transport。公司入口初始阻断，范围在主启动前预置，活跃主连接期间不扩大 scope。
- 公司数据入口和 DNS 入口长期稳定；副认证/探测完成后以身份与 revision 提交 backend。停副先 block、定向关闭该 generation 的流、确认回执，再停 transport；保留公司拒绝策略。
- 公司 DNS 本地兼容 UDP/TCP 请求，隧道上游强制 TCP；SOCKS 域名拨号同样经受控 TCP 解析，核验终端 IP 后文字 IP 拨号，避免再次调用 netstack 的 UDP DNS。
- 防止嗅探覆盖公司目标：只对批准公司网段/DNS目的地验证跳过目的地址覆盖的方案，不全局关闭主连接嗅探。官方 [目标网段跳过嗅探配置](https://wiki.metacubex.one/config/sniff/)提供配置候选，打包版本和实际流量仍须验证。
- 主副网关、company CIDR、DNS服务器单IP例外与 LAN/Tailscale/原生解析器一起做预检；服务端无include列表、split exclude与端口/协议受限条目不自动扩成全部私网许可。
- 普通第二网关优先复用现有 host-route pin 和严格恢复格式；超过预算或需要公司 CIDR 系统入口时，另做明确方案及恢复测试，不静默扩权。主短暂传输重试与控制会话重建区分，主结束时副级联停止。

## 尚未通过的门槛

1. 认证前的物理外层路径，包含未知重定向/额外网关的阻断；不能先串入主 VPN 再修。
2. 企业入口/Unix backend 的服务端身份、端口复用、旧 generation、提交/撤销竞态及主连接持续性。
3. 企业 DNS 与域名 CONNECT 在禁用隧道 UDP53 时仍正常，CNAME/AAAA/缓存/错误响应不回落公网。
4. 主 transport、root core、TUN、DNS listener不重启，主TCP/WebSocket/下载对照不中断。
5. 实际 Finder SMB、SSH、内网页面及 LAN/Tailscale/AI/国内直连对照；特别是 kernel SMB 的实际系统入口。
6. 退出/崩溃恢复、服务升级与OTA互斥、配置sidecar及回滚兼容。

上述均未执行，也未记为通过。它们在隔离原型后才可安排真实公司链路；当前生产 VPN 不作为本次审查的故障注入环境。

## 文件与保护核对

本轮只更新双连接方案和本记录。原有产品源码、用户配置、运行进程与发布产物保持原状。原始流式事件、任务范围、每段回执、Codex接受/拒绝项及保护验证保存在项目外私有目录 `/Users/willhsu/.codex/visualizations/2026/11/07/goconnect-agy-split-review/`。临时源码片段在所有调用结束并校验未被修改后清理。
