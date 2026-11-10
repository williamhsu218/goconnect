# 1.0.15 候选：名单与全局模式分流审查

## 结论与范围

当前候选为 **1.0.15 / build 75**，基线是 `9f9d69e` 加当前未提交修改。本文保留修复前审查与复现证据。用户随后授权 Codex 与 Gemini 4 Argon high 分工修复，两类缺陷已完成源码修复并通过隔离定向回归；后续修复中另发现并处理 UDP 跨目标出口复用、公司 TCP-only 适配器被跳过后回落的问题；完整构建与放行状态以 [分流修复记录](ROUTING_MODE_FIX_2026-11-10.md) 为准。没有安装候选、升级服务、切换模式或中断 VPN。

**修复前发现两类已复现缺陷：名单 fake-IP 使直连/LAN/主线路公司网段的域名流量漏匹配，以及公司副连接预检把精确父域名直连误判为子域名冲突。当前候选不宜据既有构建通过记录直接放行。** 相关代码已存在于正式 tag `v1.0.14`，这不是本次更新空闲核验或冲突检查补丁新引入的回归。后续小任务式 Agy 独立审查已返回有效结论；原始失败记录及续审结果分别保留在文末。

## 实际数据路径

当前模式是 `RoutingMode.whitelist`（按名单代理）和 `RoutingMode.global`。旧 `AccessMode.applicationProxy/companyNetwork` 文本不是当前数据面切换开关。

实际入口为 `RouteClient.start` → 默认启用 `transparentRouting/liveRouting` 的 `RouteSession` → `service-route`/特权 worker → `transparentSession` → `transparentConnection.run` → `makeTransparentConfig`。`makeLiveConfig`、PF 和旧 `handleLiveCommand` 的行为不能直接代替此路径的判断。当前 UI 的名单修改会保存到下次连接，并不在已连接会话中实时重载这些旧 provider。

普通透明会话的规则顺序是：回环与 `.local`、Tailscale、本地网络、用户直连域名/IP、主线路公司网段、进程路径、最终默认出口。有公司副连接预留时，`CompanyGate.configure` 再将受保护的公司域名/网段置于这些规则之前；配置范围冲突由预检拒绝。

## P2：名单 fake-IP 与网段例外不相容

定位：

- `Transport/cmd/goconnect-transport/managed_dns.go`：名单模式为普通系统查询返回 `198.18.0.0/16` 内的映射，等选定出口后才解析。直连域名和原生补充解析区域排除在映射之外。
- `Transport/cmd/goconnect-transport/route.go` 的 `directDomainRules`：IP/CIDR 规则带 `no-resolve`。
- `Transport/cmd/goconnect-transport/transparent_routing.go` 的 `makeTransparentConfig`：本地与 Tailscale 前缀同样带 `no-resolve`，随后才匹配应用进程。

最小触发条件：App 在代理名单内；访问一个未列入明确直连域名/原生补充区域的域名；域名的真实地址落入直连 CIDR 或已发现的 LAN 网段。系统先给 App 一个 fake-IP，前面的 IP 规则既看不到真实地址也不触发解析，最后进程规则选择 GoConnect。

已复现的两种结果：

1. `service.audit.example` 的真实地址为 `198.51.100.42`，直连规则为 `198.51.100.0/24`。名单模式仍选择 GoConnect；全局模式得到真实 DNS 地址后选择 OriginalNetwork。
2. 域名真实地址为 `10.23.0.42`，LAN 路径为 `10.23.0.0/24`。名单模式仍选择 GoConnect；全局模式选择 LAN 出口 Local0。

直接访问这两个 IP 都命中相应直连路径。明确增加域名直连规则的对照也正常。因此不是所有直连规则失效，而是延迟解析与仅按地址匹配的组合有缺口。影响包括国内 CIDR 列表不能覆盖预期域名流量，以及名单内浏览器访问某些内网域名时被送到主代理、解析或连接失败。

续审补充：Agy 独立提出相同机制还会影响主线路 `RemoteNetworks`。Codex 随后新增 4 项回环内核对照，复现 **名单外 App 通过域名访问 `10.23.0.0/24` 主线路公司网段时落入 OriginalNetwork**；同一模式访问字面 IP，以及全局模式的域名/IP 对照均选择 GoConnect。此处是 `RemoteNetworks` 的主线路规则，不是 `CompanyGate` 公司副连接，不能据此宣称副连接已确认回退或泄漏。规则选择已复现，不代表在真实物理网络观察到该内网业务成功直连。

## P2：公司副连接误拒精确父域名直连

定位：`company_controller.go:119-121` 对全部直连域名都使用对称的 `domainOverlap`，没有考虑 `domainRule.IncludeSubdomains`。

例如公司范围是 `corp.example.invalid`，直连仅为 `DOMAIN,example.invalid,DIRECT`（不包含子域名）。两条域名规则互不重叠，DNS 中精确父域名策略也不会覆盖公司子域名，但预检仍报 `company scope conflicts with direct domain`，导致合法配置无法启用。真正的父域名后缀规则或公司范围内的精确子域名依然应当拒绝。

这是 Codex 在复核公司预检时另外发现并复现的遗漏，不冒称由 Agy 独立发现。4 项纯生产函数对照中，精确父域名反例失败，父域名后缀/公司内精确子域名/无关域名三个控制项通过。影响是误阻断，未观察到因此放宽公司保护。

## 修复前模式矩阵

下表为发现缺陷时的快照，不表示修复后候选的当前结果；修复后矩阵与验收层级见上述修复记录。

| 场景 | 按名单代理 | 全局模式 | 证据口径 |
| --- | --- | --- | --- |
| 普通公网，名单内 App | GoConnect，按所选出口延迟解析 | 默认 GoConnect，系统公网 DNS 为代理 DoH | 代码与既有隔离 DNS 回归 |
| 普通公网，名单外/排除 App | 默认 OriginalNetwork，原网络解析 | 排除 App 走 OriginalNetwork，但系统公网 DNS 默认仍为代理 DoH | 代码确认；本次未做原生进程归属验收 |
| 明确直连域名 | OriginalNetwork，保留原网络解析 | 相同 | 隔离实测通过 |
| 明确直连 CIDR，访问字面 IP | OriginalNetwork | OriginalNetwork | 隔离实测通过 |
| 明确直连 CIDR，通过域名访问 | 名单内 App 漏匹配，走 GoConnect | 此次对照命中 OriginalNetwork | 已复现缺陷 |
| 已发现 LAN，通过字面 IP 访问 | LAN 出口 | LAN 出口 | 隔离实测通过 |
| 已发现 LAN，通过普通域名访问 | 未排除映射的域名可能漏匹配，走 GoConnect | 此次对照命中 LAN 出口 | 已复现缺陷 |
| 主线路 RemoteNetworks，通过字面 IP 访问 | GoConnect | GoConnect | 独立续审后的隔离实测通过 |
| 主线路 RemoteNetworks，通过域名访问且 App 不在名单中 | 漏匹配，落入 OriginalNetwork | 此次对照命中 GoConnect | 独立续审提出、Codex 复现 |
| `.local`、单标签、明确直连或原生补充 DNS 区域 | 保留真实 DNS 答复 | 保留相应原生策略 | 代码与既有隔离 DNS 回归 |
| 已启用公司预留、公司副线未连接/失效 | 公司范围保持阻断，不回退主线 | 相同 | 公司 gate 代码；本次未重连实测 |

## 需要保留的边界

- 全局排除 App 不等于其全部 DNS 自动回到原网络。系统默认公网解析仍是代理 DoH，直连域名及原生补充区域才走对应解析器。分区 DNS、国内 CDN 和断开代理时的排除 App 可用性需要分别验收，不能将两种模式描述为完全对称。
- 进程规则只覆盖配置的 `.app` 路径及其内部程序，或连接时解析出的精确可执行文件路径。App 包外的 helper/托管 CLI 不继承父进程规则；CLI 更新也可能改变运行路径。未知进程在名单模式落到 OriginalNetwork，在全局模式落到 GoConnect。这是当前既有边界，不是本次新发现的候选回归。
- 应用自带 DoH、显式指定 DNS 和其他代理软件的解析不等同于系统托管 DNS。公开 DoH 连接按进程规则走；局域网/公司 DNS 保留路径可能直连。不能仅凭系统 DNS 检查承诺任意 AI 客户端零泄漏。
- 名单公网映射不提供合成 AAAA；原生区域和所选出口仍支持真实 IPv6。AAAA-only 客户端、缓存旧映射后切换模式/线路、非 HTTP/TLS/QUIC 流量等组合仍需专项原生验收。此处列为验证缺口，没有将其判定为已复现的新缺陷。

## 原审查的修复与放行建议

本节保留当时建议；后续已经实现的部分见修复记录。原生安装与 TUN 门禁仍未完成。

先解决 fake-IP 与 IP 例外的解析时序，并将上述 CIDR/LAN/RemoteNetworks 域名场景纳入永久回归。不要仅删除 `no-resolve` 或将名单公网 DNS 全部改为原网络来消除表面失败；那会改变尚未完成进程选择时的查询去向，必须同时证明名单内 AI 解析、名单外原网络解析与代理故障不回落仍成立。按进程选择解析出口后再评估地址规则是需验证的设计方向，不是已完成的实现。公司预检还需按精确域名/后缀域名的真实交集判断，保留真正重叠时的拒绝与公司 gate 优先级。

之后在单独授权窗口验证实际进程归属、系统 DNS、国内和 LAN/公司域名、模式切换和清理。隔离 SOCKS 对照不能替代 macOS 已安装 TUN 验收。

## 本次证据

Codex 使用当前候选生成的配置与已安装 App 包内 Mihomo，关闭 TUN，DNS/SOCKS/HTTP 都是回环测试端点。只用 IN-PORT 代替进程匹配结果，并用回环记录器代替出口传输，保留生产规则优先级与 `no-resolve`。测试不访问文中的 IP 或任何真实公司服务器。

- 新组合矩阵：**10 项，8 项通过、2 项失败**；失败为 `whitelist/cidr-domain` 与 `whitelist/lan-domain`，因此该次回归命令 exit 1，未记为验收通过。
- 三组既有内核回归仍通过：`TestLiveManagedDNSSplitAndFailure`、`TestLiveManagedDNSWhitelistBrowserCompatibility`、`TestLiveManagedDNSWhitelistDefersUntilOutboundSelection`，包含代理故障不回落和名单内外延迟解析对照。
- 候选源码在审查期间没有变化。已安装应用、配置哈希和 7 个生产进程与此前修复后的保护快照一致。
- 原始证据：`/Users/willhsu/.codex/visualizations/2026/11/10/goconnect-mode-audit/`；`mode-matrix-repro.log`、`mode_audit_test.go`、`codex-analysis.json` 和基线哈希可用于复核。

## 跨 Agent 执行状态

请求固定为 Agy 的 **`gemini-4-argon-high` / `high`**，实时 `agy models` 清单确认该精确 ID 可用。Agy 获得项目外、无用户配置及运行包的源码副本，只读 plan/sandbox 审查。

首轮回执 `isError=true`：`denied_actions=1`，无最终文本；服务没有暴露具体被拒动作，不能从 `status=SUCCESS` 推断任务完成。按同一会话、同一模型/强度做一次有界续审，限制为只读并直接回复，结果 **360 秒超时，returncode=-9**。两次均没有有效审查结论，也没有实际模型/强度回执。未扩大权限、改用 accept-edits、降档或换模型。

因此，**首轮跨模型独立审查未完成**。该阶段的结论来自 Codex 的代码追踪和隔离复现；以下是用户要求继续后的独立续审，不能混淆两阶段状态。

### 用户要求继续后的独立小任务审查

保持 `gemini-4-argon-high / high`，重新核对实时模型清单。改为两个互不续接旧大上下文的小任务，通过只读 `review_agy`、plan/sandbox 接口直接提供带真实行号的必要源码片段，不提供 Codex 已有缺陷结论或复现日志，不要求模型访问文件/命令/规划工具。两项均 `isError=false`、`status=SUCCESS`，没有权限拒绝记录，并返回完整分析文本。

1. **普通透明规则与 DNS**：独立指出 fake-IP 与 `no-resolve` 前缀的交互、全局排除 App 的 DoH 依赖、包外 helper/未知进程的默认出口。它还提出主线路 `RemoteNetworks` 的扩展场景，已由 Codex 额外回环实测确认。
2. **公司副连接隔离**：从源码确认进入 CompanyGate 的流量在未连接/断开后被拒绝，不存在该 gate 内主动回退主线的路径；并明确不能仅因通用 `fake-ip-filter` 未追加公司域名就判定 DNS 泄漏，因为还有原生补充解析器、域名优先规则及 gate 内解析。其自身没有运行原生路由或公司服务器验收，结论不能扩大到所有系统路由都已证明无法绕行。

Codex 审核纠偏：名单内请求应描述为在代理出口解析，而非一概称为本机代理 DoH；全局 DoH 可能影响 CDN 选择或未缓存查询的可用性，但没有实测证明返回 IP 必定在海外；未知进程的默认出口属于已声明边界，未按新泄漏漏洞处理。公司副连接评审没有捕获上述精确父域名误拒，已由 Codex 补充。

**回执边界**：精确型号与强度请求参数已固定，平台返回了任务完成状态，但 MCP 没有提供底层实际型号/强度证明，仍记为 `not_reported`，不得写成底层型号已独立核实，也不构成候选验收通过。

续审原始证据：`/Users/willhsu/.codex/visualizations/2026/11/10/goconnect-argon-independent/`，包含两个源码片段、`review-1.json`/`review-2.json`、`remote-hypothesis.log` 和 `company-scope.log`。新增两组分别为 4 项中 3 通过/1 失败；均只使用回环或纯函数，不修改候选实现、用户配置、DNS/路由或当前 VPN。
