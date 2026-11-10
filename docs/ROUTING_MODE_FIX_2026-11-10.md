# 1.0.15 候选：按模式分流修复

## 当前状态

源码候选 **1.0.15 / build 75**，工作基线 `9f9d69e` 加已有未提交修改。本轮修复 [审查中复现的两类缺陷](ROUTING_MODE_AUDIT_2026-11-10.md)，保留此前更新空闲检查、fake-IP 地址冲突检查与直连地址导入改动。

定向隔离测试与最终 `./script/build_and_run.sh --verify` 均通过（exit 0）。**尚未安装、升级网络服务、完成原生 TUN/真实进程与 OTA 验收或发布。** 正式包仍为 1.0.14 / 74，回滚包 1.0.13 / 73；本轮没有操作当前 VPN、用户线路或订阅。

## 修复

名单模式原先只在 DNS 中返回 fake-IP；IP/CIDR、LAN 与主线路 RemoteNetworks 规则带 `no-resolve`，匹配时看不到真实地址。现在保留字面 IP 和明确域名的原规则优先级，对尚未确定真实 IP 的连接先按应用选择解析器，再检查相同的地址规则：

- 名单内默认使用代理 DNS，名单外默认使用原网络 DNS；明确原生区域与直连域名继续遵循相应解析策略。
- 地址命中后连接同一个真实 IP，保留应用原始 TLS/SNI，避免出口再次解析改变目标。
- 两侧解析器缓存隔离；解析器缺失或解析失败直接报错，不回落另一侧或系统 DNS。
- 公司副连接的域名与地址规则同时前置到两个重匹配区。公司 gate 未连接时保持阻断，不回落主线或原网。
- 原生补充域、有效的 DHCP/search 域、`.local` 与单标签保留原生语义，不对所有未知公网/AI 域名增加原网试探查询。

复审 UDP sender 时另用双目标回环测试确认：原版按来源地址复用 UDP 出口，同一 socket 先访问公网再访问直连地址，会继续使用第一个出口，名单与全局模式均复现。现在透明会话通过独立的 `goconnect-udp-destinations` 保留标记启用按目标地址/端口划分 UDP 会话；每个目标重新匹配出口及 CompanyGate，同目标后续包仍复用固定 IP。实际包内规则另发现 Mihomo 会跳过不支持 UDP 的 CompanyGate 适配器，继续命中默认出口；因此公司域名/网段均增加显式 UDP REJECT，保留 IPv4/TCP 公司业务边界。两种模式下“先公网，再公司 IP”的回环对照现均阻断，公共记录器未收到公司目标。未启用标记的上游配置保持原行为。

全局模式继续使用原有真实 DNS 和规则顺序；排除 App 的业务流量走 OriginalNetwork，系统默认公网 DNS 仍是代理 DNS。没有将两种模式改成相同的 DNS 行为。

公司预检现在区分精确域名和后缀域名：精确父域名 `example.invalid` 不再误阻断 `corp.example.invalid` 公司范围；父域名后缀、公司 apex 或公司范围内的精确子域名仍拒绝。原生补充解析器之间的后缀冲突检测保持不变。

## 固定内核与构建来源

原版 Mihomo v1.19.30 的配置不能在 IP 规则解析阶段按进程选择解析器。本轮在固定源码上使用小范围 overlay，按应用解析仅在 `goconnect-native-addresses` 和 `goconnect-proxy-addresses` 两个保留 sub-rule 启用；UDP 目标隔离单独由上述标记启用，覆盖透明名单及全局模式。普通配置不启用这些扩展。

- 上游归档：`https://codeload.github.com/MetaCubeX/mihomo/tar.gz/refs/tags/v1.19.30`。
- 上游 SHA-256：`ee8a7107707e4bd485460139b1944e7be30016393783f2b4e928c14880c8ca8b`。
- 构建版本 `v1.19.30-goconnect1`，构建标签 `with_gvisor`。
- 补丁与精确哈希：`script/patches/mihomo-1.19.30-goconnect.patch`、`script/mihomo.lock.json`。
- `build_mihomo.py` 校验源码与补丁、验证 Go 模块、运行解析器边界测试再编译；`package_app.py` 拒绝其他内核及来源不匹配的二进制。
- 包内包含来源、许可证、锁文件、补丁及配方；`prepare_release_sources.py` 将这些与对应上游归档、实际 Go 模块纳入后续发布源码附件。来源或补丁被修改的回归必须拒绝。

TCP 在出口前固定匹配 IP；UDP 按目标地址/端口划分会话，首包建立 origin→target 映射，同目标后续包复用该地址。hosts 已确定的地址不在出口再次随机抽取，避免多地址记录改变已匹配结果。没有增加长期运行的辅助进程或替换当前运行内核。

## 协作与复审

按 cross-agent-coordinator 调用 Agy，指定 `gemini-4-argon-high` / `high`；没有换模型或降低强度。Agy 在项目外隔离副本修改公司域名交集判断，并补充 5 项生产函数回归。Codex 核对允许文件清单、基线与完整 diff 后集成；按模式解析、内核补丁、构建来源链及网络回归由 Codex 实现和验收。

Agy 独立审查了核心设计，提出 UDP 后续包、hosts、双栈与失败回落的验证要求。Codex 检查 UDP sender 映射实现，补充同一会话连续 8 包测试、hosts/双栈/IPv6-only/TLS/失败回落与公司阻断检查，并修正 hosts 出口二次抽取。进一步验证跨目标 UDP 后，Codex 复现并修正了上述出口复用缺陷。首轮设计复审返回空文本，保留失败回执；同模型重试返回有效审查，未把空结果当成通过。MCP 未提供底层实际模型/effort 回执，不能将请求值冒充底层确认。

## 隔离验证与待验收边界

定向检查使用实际补丁内核，关闭 TUN。DNS、SOCKS、HTTP/HTTPS、UDP echo 与公司 gate 都是本机回环端点；IN-PORT 只代替进程归属结果，出口记录器只代替远端传输，不冒称 macOS 原生进程识别或真实公司隧道验收。

- 18 项 TCP 场景：名单/全局下的 CIDR、LAN、主线路 RemoteNetworks 的域名和字面 IP，明确直连域名及名单内外普通公网。
- 两侧 DNS 计数、同域名跨应用缓存隔离、解析失败不回落；公网 DNS 实际经 GoConnect SOCKS 载体，原生 search 域保留 LAN 路径。
- UDP 同一会话连续 8 包；同一 socket 从公网切到直连地址时分别命中两个出口，切到未连接公司范围时阻断（名单与全局对照）；IPv6-only、双栈、hosts、验证证书和原始 SNI 的真实 TLS 握手；固定拨号 IP，不发生另一侧二次查询。
- 公司未连接时，普通别名解析出公司网段也必须阻断；名单内外均不能回退两个公共出口。公司 UDP 明确拒绝，不因 TCP-only 适配器被内核跳过而回落。
- 精确父域名合法、后缀父域名冲突、公司 apex/子域名冲突和无关域名 5 项预检回归。

最终统一检查：**45 项 Python、219 项 Swift（0 失败）**，4 个 Go 包 race 检查，固定源码内核的解析器/UDP key 边界单测、release 编译、临时 ad-hoc 包的深度严格签名与动态依赖校验、配置检查、包内网络回归和 OpenConnect 隔离恢复测试均通过。最终包内 Live 检查 16.617 秒，集成检查 129.211 秒；`go vet ./...` exit 0。原生进程查询受权限限制的子项、安装服务/真实 TUN 项按条件跳过，未计为通过。

这次没有 `--run`、`--team-sign` 或 `--export`；本机依赖仍使临时包最低要求为 macOS 26.0。172 项已安装文件/符号链接、12 份配置 JSON、正式/回滚包及清单完全未变，7 个生产 App/服务/VPN 进程的 PID 与可执行路径未变。最终检查期间 278 项源码/构建输入哈希未变。临时 App、ZIP、Swift scratch、内核源码/Go cache 与 Agy 副本已清理，正式保留边界检查通过，清理 dry-run 无目标。

首轮统一检查在 UDP 补充修复前通过；为避免使用旧源码结果，最终源码已重新完整检查，上述结果取自第二轮 `verify.log` / `verify-result.json`。Agy 编辑 diff、失败/成功回执、UDP 负对照和保护快照分别留存。原始私有证据在 `/Users/willhsu/.codex/visualizations/2026/11/10/goconnect-routing-fix/`。

仍需单独授权窗口进行安装、服务升级、真实进程、系统 DNS/TUN、国内与公司内网、切换模式/退出清理及生产 OTA 验收。仅有内网 IP/CIDR 不能推导任意未知域名的公司 DNS：未被服务器补充域、search 域或明确域名规则标识的私有域名仍需提供其 DNS 范围。应用自带 DoH、包外 helper、AAAA-only 客户端与未知进程 fallback 等既有边界保留，不宣称任意 AI 服务零泄漏。

历史 AnyConnect UDP 实链路偶发超时仍按用户确认列为非阻断；本轮回环 UDP 测试不代表该历史稳定性问题已通过。
