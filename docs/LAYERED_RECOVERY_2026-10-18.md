# 分层容错与隧道自动恢复（2026-10-18）

承接 [高负载超时修复](LOAD_TIMEOUT_FIX_2026-10-18.md)。本轮在源码候选 0.11.11 / build 48 中实现，未安装或替换正式 App/特权服务，未修改用户配置，也未发起真实 VPN 连接。

## 分层处理

| 层级 | 短暂异常 | 持续或确定异常 |
| --- | --- | --- |
| GUI 与特权会话租约 | 独立后台队列续约，主线程短时卡顿不影响续约 | owner 身份变化、进程消失、Token/文件异常或租约撤销仍拒绝；GUI 持续 30 秒无响应仍撤销 |
| 本机路由与核心健康查询 | 已连接阶段只读查询超时共享 30 秒窗口；完整成功检查才重置 | 真实换网、网关改变、路由丢失、核心退出、持续无法确认健康仍停止并清理；不自动重写不确定路由 |
| OpenConnect 传输 | 同一进程使用原会话 Cookie 恢复 TLS/CSTP；原 packet socket、用户态协议栈、SOCKS 端口和路由会话保留 | 60 秒原生重试预算，外加 75 秒单调时钟总恢复上限；会话凭据被拒、证书/认证失败、服务器结束会话、取消仍终止 |
| 本地报文背压 | 暂时 ENOBUFS/EAGAIN 时等待原报文写入，不拆分/重复报文，不扩大队列，不切换到主机直连 | 每次写入最多 75 秒；真实写错误、部分写入或持续阻塞仍失败；上下文取消关闭生产 socket 并中断等待 |
| App 状态 | 显示“正在恢复连接”，开关保持开启且可关闭，保留地址和原始连接时间；恢复后回到已连接 | 清理、失败、取消后的迟到恢复事件不能重启连接；恢复成功清除已恢复的旧 TLS 故障归因 |

本轮之前 OpenConnect 参数为 `--reconnect-timeout=0`：底层仍可能完成一次立即恢复，但失败后没有等待后续重试的预算。现在使用 60 秒原生重试预算，复用 OpenConnect 的退避和会话认证机制，不重新提交密码、不生成第二个 VPN 进程。原生预算主要扣除重试等待时间，所以额外监督 75 秒总时间，覆盖 TCP/TLS/CONNECT 握手卡住的情况。截止后的退出仍有进程终止及输出收尾时间；完全停止调度时不保证精确的墙钟响应时刻。

DTLS 的协商和回退继续交给 OpenConnect，未增加禁用 DTLS、跳过证书或修改服务器 DPD 的参数。当前自动恢复限定在已有安全路由边界内；跨物理网络切换、已确认路由改变及核心进程退出仍需结束并重新建立连接，不能把 Cisco 的完整漫游能力视为本次已经实现。

## 关键实现

- `Transport/cmd/goconnect-transport/transport_recovery.go`：恢复状态与截止检查；重试不会刷新窗口，迟到成功不能复活已过期会话。
- `main.go` / `openconnect_diagnostics.go`：固定诊断事件驱动恢复状态；状态转换在诊断去重之前处理，第二次相同故障仍可正确恢复。GnuTLS 的 SSL 错误与 DTLS 错误分开，DTLS 错误不会被当作整个 TLS 会话失效。
- `Transport/internal/bridge/stack.go`：原来 1 秒拥塞即结束协议栈会破坏恢复基础；现在等待有界，并为真正阻塞的 socket 写入设置 deadline。
- `AppStore.swift` / `VPNControlState.swift` / `VPNConnectionCard.swift`：恢复时保持原连接身份，阻止并发新连接，保留主动停止入口和状态说明。没有用户配置 schema 迁移。
- 新增 `transport_recovery_test.go`、`backpressure_test.go`、`integration/recovery_test.go`、`TransportRecoveryTests.swift`；集成套件期限改为 180 秒以容纳真实 75 秒恢复截止测试。
- 测试夹具的 stdout 改为自有 pipe，并等待读取完成后报告进程结束，避免 `Cmd.Wait` 提前关闭管道导致尾部 `stopped` 事件丢失。此处是验收夹具修正，不把它当成产品故障。

## 验证证据

真实协议测试运行打包中的 OpenConnect v9.21，通过本机受信任测试证书连接合成 TLS/CSTP 服务；对端维持独立的用户态 IP 栈。流量只在 loopback 和用户态协议栈中传递，未创建系统 TUN、使用真实账号或更改系统路由。

| 验证 | 证据范围 |
| --- | --- |
| 连续两次断流恢复 | 对端断开 TLS，重连先返回 503，再恢复；同一 SOCKS 端口、同一已有 TCP 连接、同一 UDP association 恢复传输；新的 TCP 请求也通过；仅一次初始认证 |
| CPU 争用 | `nice -n 19`、8 个计算 goroutine、`GOMAXPROCS=2` 下重复上述两次恢复；生产健康循环另跑 18 个 CPU 争用场景，包含 6 秒真实 deadline 和 17.643 秒迟到结果 |
| 核心调度暂停 | 只暂停测试拥有的 OpenConnect 子进程 6.5 秒，同时发送约 560 KB TCP 数据；恢复后原连接数据一致，随后请求正常；未暂停正式 App/服务 |
| 长时间握手卡死 | 对端接受 TLS 但不完成恢复 CONNECT；真实等待 75 秒后记录 `openconnect.recovery_expired` 并停止；从测试开始到输出收尾约 77 秒 |
| 明确异常 | Cookie 拒绝、重连虚拟地址改变、恢复期间用户取消均结束连接，没有重新认证或新 `ready`；既有服务器 Idle Timeout 和不可信证书测试覆盖对应保护 |
| 背压 | ENOBUFS 持续超过原 1 秒阈值后恢复，原报文只成功写一次；持续 ENOBUFS/EAGAIN、EPIPE、阻塞写 deadline、取消仍有界失败 |
| Swift 与线程检查 | 72 项 Swift 测试通过；8 项续约及恢复测试在 ThreadSanitizer 下通过，无线程竞争报告 |
| 渲染 | 用合成 AppStore 通过 NSHostingView 实际绘制连接卡片，核对恢复状态、保留的时长/地址及开启的开关；状态/菜单测试验证可取消且不能同时开始新连接 |

`./script/build_and_run.sh --verify` 最终退出码 0：9 项构建边界检查、72 项 Swift 测试、Go 全量 race、Release 构建、严格签名、运行组件校验、14 个 TestLive 顶层用例及 9 个包内集成顶层用例通过。包内集成共 121.276 秒，其中第一轮恢复即挂起的硬截止用例 77.16 秒，调度暂停用例 10.50 秒。另有 `go vet ./...`、`go mod verify` 和新增诊断分类用例通过。

最后一次受控 CPU 争用传输测试 30.36 秒通过，8 个计算 worker 完整回收；健康循环的 18 个场景 23.69 秒通过。没有通过提高整个机器的负载到历史 load average 194 来进行破坏性复现。

3 个需要显式安装服务/root TUN 或专用子进程入口的顶层用例跳过：`TestInstalledSharedCompanyRouteAndProxy`、`TestTransparentChildProcess`、`TestInstalledTransparentApplicationModes`。另有 4 个要求更高权限的进程路径匹配子场景跳过；这不构成真实 App 分流路径验收。

诊断输出只保留固定代码和数值，不包含原始服务端文本、Cookie、密码或订阅地址。

## 最终环境与留存

- 正式 App 76 个文件、用户数据目录 54 个文件、dist 5 个文件的 SHA256 前后一致。只记录哈希，不读取输出配置正文，不访问用户密码。
- 原 App PID 55502、特权服务 PID 55593 的启动时间和路径保持不变；开始及结束时服务均 `ready / busy=false`。没有新建或中断真实 VPN 会话。
- 正式版/回滚包保留检查通过，未执行安装或 `retain`。项目清理预览为空；构建脚本自动移除项目外构建目录，其他本轮临时源码副本、研究文件和运行组件副本在证据归档后清理。
- 完整日志、CPU 测试 JSON、恢复界面 PNG、本轮源码增量、源码 SHA256、正式环境前后哈希和证据清单位于 `/Users/willhsu/Library/Logs/GoConnect/Incidents/2026-10-18-layered-recovery/`。

## 验收边界

受控测试证明上述卡顿、拥塞、断流及持续故障场景下候选行为符合预期。它不能保证任意持续时间的系统冻结都不断线，也不能替代真实 Cisco/ocserv 服务端、实际 DTLS 通道切换、root TUN、真实视频渲染和系统睡眠/漫游验收。永久无响应保护与无限容忍高负载不能同时成立。

正式版仍为 0.11.10 / 47，回滚版为 0.11.9 / 46。保持当前 VPN 和用户配置的约束下，本次不安装候选、不切换正式服务、不通过故意断开当前 VPN 来验收。

## 设计核对来源

- [Cisco 的探测重试、等待恢复和超时说明](https://www.cisco.com/c/en/us/td/docs/security/vpn_client/anyconnect/Cisco-Secure-Client-5/admin/guide/cisco-secure-client-admin-guide-new/configure-vpn-access-intro/c_connect_and_disconnect_to_a_vpn.html)。这里只借鉴分层设计，没有照抄其探测间隔作为本机超时。
- [OpenConnect v9.21 ssl.c](https://gitlab.com/openconnect/openconnect/-/raw/v9.21/ssl.c)：原生重连使用同一会话，重试预算扣除等待时间，Cookie 失效会结束会话。
- [OpenConnect v9.21 cstp.c](https://gitlab.com/openconnect/openconnect/-/raw/v9.21/cstp.c) 与 [gnutls.c](https://gitlab.com/openconnect/openconnect/-/raw/v9.21/gnutls.c)：CSTP 成功及 SSL 读取错误的固定事件文本；再用本机实际 v9.21 二进制运行验证。
