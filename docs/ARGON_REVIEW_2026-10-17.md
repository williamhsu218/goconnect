# Argon 分析核查与修复（2026-10-17）

来源：[用户提供的分析文档](https://docs.google.com/document/d/1zNeyrB-Fh59K0HimgrjiAPvrZA8eSZR9DI_CF0emO6k/edit)。核查基于本仓库当前源码和本机只读运行状态。Google Drive 连接器无此文件权限，正文通过用户已有的 Chrome 文档会话读取，未更改原文。

源码候选：0.11.11 / build 48。当前正式安装和保留包仍为 0.11.10 / build 47，回滚包为 0.11.9 / build 46。本轮不安装、不发布、不更新特权服务，不进行真实 VPN 断线或路由注入。

## 逐项判断

| 原文问题 | 核查结论与处置 |
| --- | --- |
| Go 清理器只接受 schema 5 | 正确，但影响范围是旧应用组维护，不代表当前透明分流必定连接失败。支持保留相同路径结构的 5–7，未来或未知版本继续拒绝；测试覆盖跨线路白名单及直连名单保护。 |
| company 残留会话无法自愈 | 正确，原拦截是明确的安全策略。新增受限恢复：持锁、root 路径及权限校验、严格解析、确认所有服务核心及目标网卡均已退出、先备份账本，再清理精确归属的路由。记录损坏、备份/读表/删除失败或仍存活时继续保留现场。正常 company 清理也改为精确查表，并复核删除失败后的实际状态。 |
| IPv4 网关与订阅限制 | 正确。订阅解析双栈地址，维持双栈时优先 IPv4，允许 IPv6 单栈节点；启用节点核心 IPv6。外层固定路由、查询、恢复账本支持 IPv6 /128 和对应物理下一跳；原网络支持 IPv6 默认路由回退。实际请求通过 IPv6 回环节点验证，真实 IPv6 VPN 与纯 IPv6 物理网络仍需另行验收。 |
| 版本重复手填 | 正确。VERSION.json 成为唯一维护源，生成 Swift/Go 常量并由构建和 CI 检查；打包版本、构建号和部署目标直接读取该文件。dist/releases.json 继续只记录已验收包，不能充当源码版本源。 |
| C 启动器仍编译打包 | 正确。删除启动器源码及其无调用入口，不再编译、复制或签名；包内确认无 GoConnectLauncher。保留历史来源署名。 |
| Go 旧会话与 PF 代码均可删除 | 部分正确。删除无生产调用方的旧 routeSession、routingCheck、handleCommand 和 launchCapturedApp 执行器；保留当前透明分流使用的协议校验、应用路径规则、进程快照、锁、配置及探针工具，以及卸载旧组所需的 GC。旧模式配置/回归工具仍在，未宣称所有遗留代码已经清空。 |
| Swift 代理、自检与热更新状态闲置 | 正确。删除 proxyFront、proxyPort、代理检测器、自检客户端及其处理器、pendingAppUpdate 和 RouteClient 的未用接口；同步移除相应不可达界面分支。应用名单继续断开后编辑、重连生效。 |
| AccessMode、Core 旧接口全是废弃接口 | 不宜一并删除。AccessMode 仍是 Codable 兼容字段，旧 companyNetwork 配置依赖它迁移为 companyRoutesEnabled；保留这些迁移与既有 Core 公共辅助接口。 |
| 最低系统版本膨胀 | 部分正确。Swift 目标确为 14；Go 的 CGO 编译与链接未统一目标。现已统一且实际 App、Transport 均记录 14.0；当前 Homebrew OpenConnect/Mihomo 和动态库要求 26.0，因此候选包如实要求 26.0，不能声称支持 14。新增 build-requirements.json 列出每个组件的实际要求。旧已验收包继续保持原要求 27.0。 |
| 预生成 ICNS 未使用 | 正确。打包使用 PNG 重新生成内容命名的图标，删除冗余 Packaging/GoConnect.icns。 |
| 应用扫描不限深度 | 正确。限制为根目录下最多 3 层，继续跳过 App 包内部；深层应用仍可手动添加。合成目录测试覆盖边界层、越界应用和内嵌 helper。 |
| dist 忽略规则与保留策略矛盾 | 保留与 Git 跟踪是不同边界，不是运行缺陷。git clean -fdX/-fdx 确会破坏本机保留包，已明确警示并要求使用既有安全清理器。未把 ZIP 改成普通未忽略文件，避免引入 git clean -fd 的新风险；未把发布包擅自提交到 Git/LFS。 |
| 空目录与 .DS_Store | 3 个 .DS_Store 已按安全清理器先预览后移入废纸篓。空目录不是运行缺陷；未为目录整洁删除源码、维护边界或历史证据。 |
| CI 模板未启用 | 正确。模板移至 .github/workflows/check.yml，增加版本一致性检查与项目外 Swift scratch；使用支持当前 SDK 的 macos-26 runner。只完成本地配置，尚未推送，也没有云端运行成功证据。runner 依据：[GitHub 官方镜像目录](https://github.com/actions/runner-images)。 |
| 历史文档与未跟踪文件 | 不是缺陷。保留已有发布验收记录和用户原有 3 个未跟踪文件；新增文档索引供定位，不把历史验证结果合并成未经核对的新 CHANGELOG。 |
| 缺少自有代码 LICENSE | 事实成立。许可证选择属于权利人授权决定；本轮不擅自添加许可证，不改变第三方许可或声称再分发合规已经完成。 |

## 验证证据

- `./script/build_and_run.sh --verify --export /tmp/GoConnect-argon-review-20261017` 完整通过：9 项构建/清理边界测试、61 项 Swift 测试、Go race、原生构建、严格签名、动态依赖及包内组件配置检查。
- `go vet ./...` 与 `go mod verify` 通过。
- 新增 company 恢复测试覆盖路由已消失、被替换、只有覆盖前缀、精确归属、活跃核心/网卡、符号链接、无效记录、备份/读表/删除失败；仅使用合成状态与命令响应。
- 内置 OpenConnect 的隔离认证、CSTP、TCP/UDP、证书拒绝及断开分类通过。
- 新增 `TestSubscriptionIPv6NodeAndLease` 通过：打包后的 Transport 启动 Mihomo，通过 IPv6 回环 HTTP 节点实际完成请求，并验证租约关闭。未访问真实订阅、DNS 或公网。
- 候选包读回：0.11.11 / 48，App 与 Transport 的 Mach-O 最低目标均为 14.0；第三方组件为 26.0，Info.plist 如实为 26.0；无旧 C 启动器。
- 临时 `--run` 原生界面已验收：显示 v0.11.11、扫描列出 100 个应用，名单保存、搜索、刷新后状态保留、直连名单及诊断页正常；诊断如实提示服务需更新。预览使用临时数据，随后退出并自动清理。
- 预览前后正式数据目录的 54 个文件哈希完全一致，正式服务仍 ready/busy，保留包哈希和版本不变；项目安全清理器最终无待清理项。
- 正式安装、服务和真实 VPN 的新行为尚未验收；需要单独的安装授权与适用的连接测试。macOS 14–26、Intel 与实际 IPv6 外层路由未完成设备验收。

详细临时验证日志：`/tmp/goconnect-argon-verify-final-20261017.log`。导出候选包在本轮验收结束后清理，不更新 dist 保留对。

## 补充核查：IPv6 网关查询与健康检查

用户截图指出的两点均有修复价值。第一点是上一轮遗漏的确定缺陷：启动前和周期健康检查仍有不带地址族的 `route get`。本机只读实测 `route -n get ::1` 返回 68 / bad address；`route -n get -inet6 ::1` 成功、接口为 lo0。此前的 IPv6 回环节点测试覆盖订阅核心的数据请求，没有覆盖特权透明会话的这两个系统路由查询，不能据此认定整条 IPv6 VPN 外层路径已经通过验收。

本轮补齐：

- 将外层地址查询统一收敛到 `gatewayRouteInterfaceWith`，显式选择 `-inet` / `-inet6`，诊断类别相应为 route.get4 / route.get6。启动前和后续核查使用同一入口。
- 每 5 秒健康检查复用 `transparentGatewayReady(r.Gateway, outerOriginal)`，同时核对精确的静态主机路由、同地址族物理默认下一跳和实际转发接口。物理默认路由必须是不带 IFSCOPE 的真实默认项；不能用 GoConnect 自己保留的 scoped 默认路由证明旧网关仍有效。IPv4 默认路径保持不变、IPv6 下一跳单独变化时，也会被识别并进入现有停止及清理路径。
- 静态主机路由目的地址按 Prefix 比较，下一跳按数值地址比较，兼容 IPv6 压缩、展开和 /128 写法；接口、前缀长度、路由标志和 IPv6 zone 仍严格核对。清理/恢复和 scoped 默认项使用相同的下一跳比较，避免识别成功却无法按同一归属规则清理。
- 新增本机 IPv6 回环 `route get` 读取验证，以及 IPv4 不回归、IPv6 下一跳变化、旧 scoped 默认项掩盖、固定项丢失/被替换、错误出口、地址写法和查询超时保留的回归测试。故障变化使用合成路由表，不改动真实路由。

本轮继续维护 0.11.11 / build 48 源码候选，不安装或改写已验收的正式版、回滚版和特权服务。真实 IPv6 VPN 与物理 IPv6 换网场景仍需独立设备验收。

补充修复后，`./script/build_and_run.sh --verify` 完整通过：61 项 Swift 测试、Go 全量 race、构建、严格签名、组件依赖检查及打包后隔离集成测试（包括 IPv6 节点实际请求）；`go vet ./...` 和 `go mod verify` 通过。本机只读 IPv6 回环查询也纳入 Go 测试。日志为 `/tmp/goconnect-ipv6-followup-verify-20261017.log`。临时构建已自动删除，安全清理器无待处理项；54 个正式数据文件哈希不变，正式网络服务仍为 ready/busy，正式与回滚包保持不变。

后续完整调用链复查、追加修复与 19 个故障场景证据见 [连接生命周期复查](CONNECTION_LIFECYCLE_AUDIT_2026-10-17.md)。
