# 2026-10-17 视频渲染期间两次断线诊断

后续源码修复及隔离验收见 [2026-10-18 高负载超时修复](LOAD_TIMEOUT_FIX_2026-10-18.md)。下文保留诊断时的现场与判断。

本次只检查日志、进程、版本、系统电源记录与相关源码；没有安装候选、重启 App/特权服务、修改配置、切换 VPN 或做负载复现。时间统一为北京时间（UTC+8），原始 JSON 使用 UTC。

## 结论与证据边界

两次分流会话的直接退出原因已经分别确认：第一次是外层网关检查中的本机路由查询超时，第二次是 GUI 控制租约超过 5 秒未更新。两次均伴随系统负载急升，支持“渲染高负载导致本机控制操作延迟，严格超时策略触发退出”的判断。

这不是依据风扇声音作出的判断。App 实际采集的一分钟 load average 第一轮最高 194.45，第二轮到 93.86，本机有 12 个逻辑 CPU。load average 是负载指标，不是 CPU 百分比，不能仅据此区分 CPU 争用、内核等待或 I/O 阻塞，也不能归责到某一个历史渲染子进程。现有日志未逐进程采集当时的 CPU、GPU、线程和 I/O；具体资源争用来源仍未证实。

## 已确认的时间线

| 北京时间 | 记录 | 含义 |
| --- | --- | --- |
| 23:36:45 | App load average 34.23 | 第一轮负载开始急升；此前 23:35:14 为 3.94 |
| 23:37:39.172 | worker 94119，`system.command_timeout`，`route.get4`，operationID 163931，durationMs 7748，timeoutMs 6000 | 本机路由查询超过 6 秒的配置期限；定时器也受系统调度影响，记录时已过去 7.748 秒 |
| 23:37:52.012 | `session.exit_first`，stage `health_gateway`，reason `gateway_query_failed`，check `system_timeout` | 第一次分流会话开始退出，直接原因是查询超时 |
| 23:37:52.999 | 相同 operationID 163931 的 `system.command_late_result`，check `success`，exitCode 0，durationMs 17643 | 同一查询最终成功，只是结果来得太晚；超时不等于已经确认网关失效 |
| 23:38–23:40 | 6 条 `transparent.route_cleanup_failed`，均为读表超时 | 退出后恢复路由的查表也受到延迟；首因与清理失败是两层问题 |
| 23:39:17 | App load average 194.45 | 第一轮峰值 |
| 23:39:52 | `connection.disconnect_requested`，随后 OpenConnect exitCode 0 | GUI 侧主动断开晚于分流服务最初失败约两分钟，不能把它当作第一轮故障的起点 |
| 23:40:19.671 | 新 worker 53847，`session.recovery_ok` | 重连时成功恢复上一会话遗留的 6 项路由记录 |
| 23:40:21 | `phase.connected` | 第二轮连接已建立 |
| 23:44:13.693 | `transparent.health_failed`，controller_timeout，durationMs 1200，timeoutMs 1000，value 1 | 第二轮先出现一次控制器超时；这不是最终退出首因，也不是三次核心失败 |
| 23:44:23 | App load average 45.60 | 第二轮负载继续上升 |
| 23:44:26.187 | `session.exit_first`，stage `lease`，reason/check `lease_expired`，durationMs 5905，timeoutMs 5000 | 租约文件已 5.905 秒未续期，超过 5 秒阈值，触发第二次分流退出 |
| 23:44:35–23:45:27 | 5 条读表超时导致的清理失败 | 第二轮清理约持续一分钟；最终摘要不能代替 23:44:26 的首次退出时间 |
| 23:45:24 | App load average 93.86 | 第二轮高负载 |
| 23:45:27 | `connection.unexpected_disconnect`，source 2；随后 transport.stop_requested，OpenConnect exitCode 0 | GUI 收到本机分流失败后停止 VPN；OpenConnect 正常退出是后续结果 |

## 版本、运行证据与排除范围

- 实际安装：`/Applications/GoConnect.app`，0.11.10 / build 47；App PID 55502、特权服务 PID 55593 均从前一天持续运行。本轮没有发现它们在故障点重启。
- App 内 Runtime 与正在运行的特权服务 `GoConnectTransport` SHA256 一致：`f4d297fa469e4d5e7a2de65d0b16dc8dc0405bb8a9618f0e67cbb6c21114a244`。
- 工作区已经是尚未安装的 0.11.11 / build 48 候选，存在既有未提交修改。本报告没有把候选代码当成已安装版本；退出首因来自实际 0.11.10 服务日志。
- 故障窗内未记录 OpenConnect 服务器断开、会话到期、DPD 超时、TLS 读写错误或异常信号。两轮 OpenConnect 都在后续停止请求后 exitCode 0。没有证据把本次首因归为服务器踢线。
- App 日志未记录同期 `network.unsatisfied`、系统 sleep/wake、主线程 stalled/recovered 或 scheduler.gap。`pmset` 在 23:30–23:49 没有 Sleep/Wake/DarkWake/ThermalEvent 类别事件；断线窗前最后一次内存压力变化为 23:24:41 恢复 normal。不能据此声称系统完全没有调度延迟，但没有睡眠或严重内存压力作为首因的证据。
- `pmset -g therm` 未记录温度或性能告警；风扇高速运转本身不能证明过热保护。
- 采集末尾 `service-status` 为 ready / busy=false，未见 OpenConnect、分流 worker 或 Mihomo 活跃进程，表示当时已无活动 GoConnect 会话。没有主动发起重连。

## 相关代码与修复方向

当前候选仍保留这些机制，与正式日志中的阈值一致：

- `Sources/GoConnect/Services/RouteClient.swift:42`：每秒触发的 Timer 在主 RunLoop 上运行，再回到 MainActor；`tick()` 更新租约文件时间。高负载下主线程调度或文件元数据更新延迟，会影响续期。日志确认续期年龄超标，但没有直接记录究竟哪一步延迟。
- `Transport/cmd/goconnect-transport/route.go:313`：租约年龄超过 5 秒立即返回 `lease_expired`，随后才检查 owner 身份；不区分仍存活但续期暂迟和 owner 已消失。
- `Transport/cmd/goconnect-transport/system_command.go:57`：本机系统命令统一配置 6 秒上限。
- `Transport/cmd/goconnect-transport/transparent_session.go:285`：网关查询出错直接结束会话；核心控制器检查另外有连续三次失败计数。当前源码的完整路由计划检查也需要纳入同样的高负载设计评估。

建议修复分两处进行：租约续期从 UI 主线程解耦，并在验证 owner 身份、明确撤销和有限宽限期的前提下容忍短暂迟到；健康检查区分“无法及时确认”和“已经确认路径改变”，对只读查询超时进行有界重试。真实路由变化、owner 消失、显式撤销和认证失败仍应及时退出，不通过静默直连规避故障。

清理的路由读表超时也应有独立重试与可恢复账本。不能把统一超时简单无限延长，也不能删除租约和出口校验。修复验收应先使用隔离时钟/慢命令验证 5.905 秒续期迟到、6 秒查询后迟到成功、清理超时及真实 owner 消失等情形，再在用户正常渲染中观察自然运行；本轮仅完成诊断，没有实施或宣称修复验收。

## 保留证据

仓库外私有目录：`/Users/willhsu/Library/Logs/GoConnect/Incidents/2026-10-17-render-load-2354/`（0700；文件均 0600）。

- `app.jsonl`、`app.jsonl.1`：采集时的 App 日志副本。
- `service.json`：已安装组件导出的固定事件、数值及脱敏字段，含 `session.exit_first/final`、迟到命令、清理结果和 worker 最终退出摘要。
- `timeline.json`：故障窗口 App/服务事件按原始 UTC 时间排序，保留来源和 operationID，便于核对写入顺序受拥塞影响的记录。
- `status.json`、`manifest.json`：采集状态、版本/电源检查摘要与上述文件 SHA256。

没有采集密码、Cookie、账号、订阅内容、服务器原文或请求正文。没有手工改写原始应用日志或正式包；仅新增本诊断文档及仓库外证据副本。
