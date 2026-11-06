# 连接、健康检查与退出清理复查（2026-10-17）

源码候选为 0.11.11 / build 48。用户要求保留正式版与当前 VPN。本轮没有安装、更新特权服务、重启正式 App、修改真实配置或注入系统路由。

## 核对的生产调用链

| 阶段 | 实际入口与下游 | 核对点 |
| --- | --- | --- |
| 发起连接 | `AppStore.connect` → 服务就绪 → `TransportClient.start` | 配置校验、异步取消、旧连接任务隔离、进程代次、认证输入管道 |
| AnyConnect | `session` → OpenConnect → `packetPipe` → bridge SOCKS | 证书/认证、CSTP、TCP/UDP、stdin 租约、末尾诊断与子进程组回收 |
| 订阅 | `subscriptionSession` → 重新精简节点 → 地址解析 → Mihomo SOCKS | IPv4/IPv6、代理认证字段、实际节点请求、取消与核心退出 |
| 透明分流 | `handle(ready)` → `RouteClient.start` → `serviceRoute` → `handleServiceConnection` → `serviceWorker` → `transparentSession` → `transparentConnection.run` | UID/PID/签名/私有控制文件、独占锁、旧会话恢复、双栈外层网关、直连出口与接管路由 |
| 健康检查 | `transparentConnection.run` 的周期分支 | 原网络、外层固定项、物理默认下一跳、出口接口、完整接管/直连路由计划、TUN 与认证控制器；连续三次核心失败才退出，中间成功重置计数 |
| 主动断开/退出 | `disconnect` / `applicationWillTerminate` → 租约撤销与 stdin EOF → worker/隧道清理 | 先撤接管路由再停止核心，精确归属、删除后读回、保留失败账本、最终 failed/stopped 状态 |
| 非正常退出 | GUI owner 消失、租约过期、socket 断开、核心退出、panic → 同一清理路径 | 首个原因保留、清理错误独立记录、超时隔离；panic 不能写成成功停止 |
| 遗留兼容入口 | `serviceWorker` → `companySessionForOwner` | 当前 GUI 已走透明分流；仍可达的兼容入口也核查新增、清理、取消、路由/核心健康与残留恢复 |

## 本轮确认并修复的问题

1. **Darwin 实际接口路由格式被误判。** 本机只读路由表的网关列可为 `utun100`，旧逻辑只接受 `link#…`，可能遗漏自有路由并丢弃账本。现同时识别这两种格式，继续严格检查目的前缀、接口、scope、标志和下一跳。
2. **命令退出码不能证明路由新增/删除生效。** 新增前查冲突、先记意图，新增后精确查表确认所有权；删除后也查表。Darwin `route` 的部分失败会打印信息却退出 0，依据为 [Apple route 源码](https://raw.githubusercontent.com/apple-oss-distributions/network_cmds/main/route.tproj/route.c)。`File exists` 不取得所有权，未确认的超时/迟到结果保留 pending 意图并暂停自动恢复，避免清除并发服务路由。
3. **健康检查遗漏完整接管与借用直连路由。** 核心和 TUN 存活不代表路由仍有效。现在同时检查所有计划项；借用的系统 scoped 默认路由参与健康检查但不进入清理所有权账本，允许内核动态默认项没有 STATIC 标志。scoped 冲突按接口区分。
4. **IPv6 直连网关查询错误被吞掉。** 现在传播查询失败，在无法确认双栈出口时停止安装路由。
5. **取消可能在启动期间继续安装路由。** 启动核心前、每次路由新增前、发布 ready 前重新检查 ctx 和真实租约；取消后进入正常回收，不因后续健康查询抢先报告其他原因。
6. **首份账本写入失败残留不可读目录。** 初始状态保存失败时立即移除尚无进程/路由的临时会话目录，避免阻断后续所有连接。
7. **清理失败可能被当成成功断开。** `RouteClient` 保留 failed，启动过的监督进程意外退出也报告失败；`AppStore` 在 stopping 阶段保留错误；目录删除失败也返回失败。panic 的最终状态明确为 failed，同时保留原 panic 传播和脱敏首因。
8. **排队的旧连接任务可进入新连接。** `AppStore` 增加连接代次；异步工作开始、等待返回和异常分支均核对代次，断开使旧任务失效。
9. **认证代理的 username 被精简掉。** 保留 HTTP/SOCKS5 支持的 username 字段，并用需要认证的 IPv6 回环 HTTP 代理完成实际请求回归。
10. **兼容 company 入口沿用不验证新增结果的逻辑。** 复用新增与 pending 所有权确认，补上取消检查、核心/路由健康和删除读回；清理路由不再因核心停止失败而完全跳过，失败仍保留现场。

## 验证方法与边界

- `transparent_session_test.go` 直接执行生产 `transparentConnection.run`，只替换系统命令、时钟和核心启动边界；控制文件、租约、JSON 配置、磁盘账本、状态发布、退出诊断和 unwind 使用生产实现。启动独立 `/bin/sleep` 进程组验证停止逻辑，路由响应为合成内核表，绝不调用真实 route add/delete。
- 19 个生命周期场景：主动取消、租约撤销、安装途中取消/撤销、ready 前换网、IPv6 下一跳变化、接管路由丢失、借用直连丢失、核心退出、连续健康失败、间歇失败后重置、启动超时、核心启动失败、配置写入失败、删除退出 0 但无效果、清理查表超时、目录删除失败、核心停止失败、ready 后 panic。核对首因/终因、最终状态、清理与保留结果、借用路由保护和日志隐私。
- 路由与恢复测试还覆盖：原生 IPv4/IPv6 表格式、scope 和接口、外来路由/覆盖前缀、碰撞、未确认迟到结果、重复清理顺序、初始账本失败、IPv6 查询失败、活跃核心/网卡、符号链接、无效账本和备份失败。
- Swift 使用私有临时 runtime/control/data 目录与合成 helper 验证取消后重连、清理失败显示失败、正常断开成功；没有操作正式服务。
- 打包集成通过真实 bundled OpenConnect/Mihomo 与本机合成服务器验证认证、CSTP、TCP/UDP、证书拒绝、断开原因、IPv6 匿名/认证 HTTP 节点和 EOF 租约。涉及真实 root TUN 的用例按既有门禁跳过。
- 本轮没有做真实 VPN 重连、纯 IPv6 物理网络切换、root 路由注入、GUI 强杀或睡眠唤醒。上述隔离与只读证据不能替代这些设备验收，也不能声称所有订阅协议均已真实验证。pending 意图保留是有意的保守隔离，需导出诊断后人工核对，不能自动当作已清理。

## 最终验证结果

- `./script/build_and_run.sh --verify` 完整通过：9 项构建/清理边界测试、64 项 Swift 测试、Go 全量 race、原生生产构建、严格代码签名与包内运行组件/依赖检查。打包后 14 个 TestLive 顶层用例及 5 个可执行集成顶层用例通过，其中 IPv6 节点含匿名与认证两个子用例。3 个 root/子进程入口按门禁跳过，未安装或启用真实 TUN 测试。
- 最终源码另跑 `go test -json -race -count=1 -timeout 60s ./...`：210 个通过条目（包含子用例），16 个环境相关跳过，无失败；其中生产生命周期的 19 个故障场景全部通过。
- `go vet ./...` 无输出错误，`go mod verify` 报告 all modules verified；`git diff --check`、版本常量检查通过。
- 正式 App 的 76 个文件与真实数据目录的 54 个文件哈希完全不变；开始时记录的 8 个正式相关进程仍是原 PID/路径；正式服务只读查询仍为 ready/busy。
- 保留包仍为 current 0.11.10 / 47 和 rollback 0.11.9 / 46，SHA256 与开始时一致。没有导出或保留新分发包。
- 构建临时目录自动移除；安全清理器先列出、再将 5 个 `.DS_Store` 移入废纸篓，最终预览无待清理项。

日志：完整验证 `/tmp/goconnect-lifecycle-verify-20261017.log`，生命周期 race `/tmp/goconnect-lifecycle-go-tests-20261017.log`，最终 Go JSON `/tmp/goconnect-lifecycle-final-go-20261017.jsonl`，正式环境比对 `/tmp/goconnect-lifecycle-preservation-20261017.json`。临时日志不进入仓库，不保留候选分发包。
