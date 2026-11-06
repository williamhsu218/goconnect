# 分流退出诊断

本次源码增强保留退出的因果顺序：先记录触发退出的分支，再记录路由恢复、核心停止和临时目录清理的结果。清理失败仍按原有恢复策略处理，但不会覆盖首次退出原因。路由、连接策略和用户配置格式不变。

这些字段从 0.11.10 起提供，需同时更新 App 和本机网络服务后生效。更新前先断开 VPN 并退出旧版；在新版“诊断”中更新服务并完成系统管理员授权。线路配置、订阅和钥匙串设置保持原样。正在运行的旧服务不会自动加载新日志逻辑，0.11.9 及更早版本也无法补回过去未记录的首次原因。

当前保留正式版为 [0.11.14](RELEASE_0.11.14.md)，唯一分发回滚版为 0.11.13；应用及服务已更新，补充 UDP 队列诊断与恢复触发分支。真实 VPN 验收及强制重连期间一次 UDP 超时的边界见 [安装验收记录](CANDIDATE_INSTALL_0.11.14.md)；用户已明确接受该边界并授权正式收尾。

## 如何读取

在“诊断”中导出日志，按 UTC 时间与 `pid` 对齐 App、服务和工作进程记录。系统命令的 `operationID` 只在同一 PID 内唯一。不要把上一会话的摘要当成本次退出，先核对时间与 PID。

| 事件 | 含义 |
| --- | --- |
| `session.exit_first` | 清理前确定的首次退出原因，后续错误不能替换它 |
| `session.exit_final` | 同一个首次原因，加上清理失败数量、核心停止结果和最多 33 条清理细节；`value` 是会话持续毫秒数 |
| `system.command_timeout` | 系统命令达到现有 6 秒上限；包含命令类别、耗时、上限与操作编号 |
| `system.command_failed` | 命令已返回失败；有实际结果时包含退出码、信号或 errno |
| `system.command_slow` | 成功命令耗时至少 1 秒，便于观察超时前的系统延迟 |
| `system.command_late_result` | 调用方超时返回后，子命令才结束；与超时事件使用相同操作编号 |
| `transparent.route_cleanup_failed` | 具体路由的清理失败阶段与命令结果 |
| `transparent.cleanup_journal_failed` | 清理状态写入失败及 errno |
| `transparent.core_stop_failed` | 核心停止失败，独立于路由清理结果 |
| `transparent.cleanup_directory_failed` | 清理临时目录失败及 errno |
| `worker.exited` | 服务观察到的工作进程退出码和信号，可辅助定位未完成退出记录的异常终止 |
| `diagnostics.write_max_ms` | 最近心跳区间内最大的日志写入耗时，包含快照、追加及同步；不是磁盘故障的单独证明 |
| `transparent.health_deferred` | 只读健康查询超时，仍处于共享的 30 秒容错窗口；不等于确认换网或已经断线 |
| `transparent.health_recovered` | 完整健康检查恢复；`durationMs` 是本次不确定窗口持续时间，`timeoutMs` 是窗口上限 |
| `openconnect.reconnecting` / `openconnect.reconnected` | 已连接会话进入恢复 / CSTP 重新连接成功；重复失败不重置总恢复计时 |
| `openconnect.recovery_expired` | 达到 75 秒总恢复上限，随后仍需等待进程退出和清理；不是无限重试 |
| `openconnect.reconnect_failed` | 固定文本分类得到的重连失败线索，不单独证明包装进程退出；0.11.14 起也可独立触发已连接会话的恢复窗口 |

`details.stage` 标记连接准备、网络检查、核心启动/健康检查、租约或清理等阶段。`reason` 是导致该事件的固定原因，`check` 保留底层检查的固定结果。例如，`reason=network_query_failed, check=system_timeout, command=route.get4` 表示读取原网络的命令超时，不能解释为已经确认网络发生切换。

首次原因明确区分：核心退出（`core_exited`）、启动等待超时（`core_start_timeout`）、连续三次健康失败（`core_unresponsive`）、租约缺失/读取失败/令牌不匹配/过期/时间超前、拥有者消失/改变、上下文取消及 panic。panic 只记录类别，不记录任意载荷，且继续传播。健康检查进一步区分 TUN 缺失或未启用、控制端连接失败或超时、HTTP 状态异常、解码失败及 TUN 功能关闭。

`route_missing` 是 `session.exit_first.details.reason` 的值，表示必需路由复核未通过；`route_add_incomplete` 是 `transparent.route_cleanup_failed.details.reason` 的值，表示路由仍有未确认的添加意图，不能按已确认拥有的路由直接删除。它们不是独立事件名。查询本身超时与确认路由丢失须结合 `check` 区分。

路由清理使用 `reason=read_failed/delete_failed/recheck_failed/delete_still_present` 区分读表、删除及复核。`routeIndex` 是原清理清单中从 0 开始的序号，`family/prefixBits/scoped/interface` 标明路由类别、前缀长度、是否为接口作用域及网卡。不会写入目的地址或网关。日志中的数组顺序按实际清理顺序保留，不能用最终剩余清单重新推算原序号。

## 保留与隐私

### UDP 桥接诊断（0.11.13 起）

App 将桥接诊断保存在独立的 `bridge.jsonl/.1`，事件名为 `bridge.trace`，随 App 日志导出。只追踪显式标记的 STUN 测试事务以及固定失败信息，不记录普通报文正文或目标地址。未标记事务的错误记录中 `transaction=0`，不能用它关联某个用户请求。

| 字段 / 阶段 | 含义 |
| --- | --- |
| 外层 `pid` / `details.pid` | 分别为落盘 App 与产生事件的桥接进程；不能混用 |
| `transaction` / `flow` | 匿名探针事务号 / 本地目标连接编号，结合会话与桥接 PID 关联 |
| `timeMicros` | 观测时间的 UTC 微秒数；并发阶段日志可能交错，不按文件行序推断严格调用顺序 |
| `bytes` / `packetKind` | 对应阶段字节数 / STUN 类型（1 请求、257 成功响应、273 错误响应；0 表示不适用） |
| `outcome` / `errno` | 固定结果分类 / 系统错误号；不包含任意错误文本 |
| `sequence` / `dropped` / `appDropped` | 桥接序号 / 桥接累计丢弃 / App 接收落盘队列累计丢弃；分析前必须检查覆盖窗口与缺口 |
| `socks_receive`、`udp_write_start`、`udp_write` | 收到客户端报文、开始写入、写入用户态 UDP socket 的结果 |
| `pipe_write_start`、`pipe_write` | 开始向 OpenConnect 本地 packet socket 交付、交付结果；成功不单独证明网关或远端收到 |
| `pipe_read`、`stack_deliver`、`udp_read`、`socks_send` | 从通道读回、交付协议栈、目标 UDP socket 读回、回复 SOCKS 客户端 |
| `udp_dial` | 目标连接创建失败 |
| `udp_queue`（0.11.14 起） | 队列 / 目标数达到上限时拒绝新报文（`io_error` + ENOBUFS），或目标退休时丢弃未发送的排队报文（`canceled`）；不表示 App 日志队列丢弃 |

桥接诊断保留有界异步队列和每进程 8,192 条详细记录上限；`status` 定期报告序号及丢弃计数。App 导出对每个 App / bridge 文件仅保留末尾 128 KiB，可能截断较早的完整事务。日志缺失或丢弃不能被当作网络丢包，完整长测应持续归档轮转文件。读回收到 STUN 错误响应也不能算成功业务响应。

服务日志仍使用两个不超过 1 MiB 的轮转文件。首次及最终退出记录有独立的两个队列位置，普通日志队列拥塞不会占用它们；优先记录也可能使文件中的写入顺序与事件时间顺序不同，分析时以时间和操作编号对齐。

`worker.exit.json` 在服务的 `diagnostics` 目录中原子更新，保留最近的退出摘要，不随会话临时目录删除或 JSONL 轮转消失。仅有首次记录而没有最终记录，说明最终摘要未完成，不能视为清理成功。文件权限为 `0600`，导出通过已有授权服务接口读取固定文件名。

只记录允许的事件、类别、网卡名及数值。禁止记录完整命令参数、原始 stdout/stderr、错误字符串、响应正文、令牌、密码、订阅地址、目的地址或用户路径。日志排队和落盘保持异步，记录故障不等待慢磁盘；普通日志丢弃数量仍在 `process.heartbeat.value` 中报告。

SIGKILL、进程崩溃、断电或文件系统始终无法写入时，进程无法保证完成自己的退出摘要。此时保留服务观察到的信号、最后成功落盘记录及 macOS 系统日志，不能仅凭记录缺失推断具体外部杀进程者或内核故障原因。

## 开发验收

新增隔离测试覆盖真实子进程非零退出和信号、阻塞命令超时与迟到结果、成功慢命令、租约各失败分支、HTTP/TUN 健康失败分类、清理读表/删除/复核/日志写入失败、panic 传播、队列拥塞、快照轮转及不安全文件拒绝。组合场景验证“命令超时 → 清理失败 → 临时目录清理与普通日志轮转 → 首次原因仍可读”。测试只使用临时文件、子进程、回环 HTTP 和模拟路由，不修改真实网络。

完整临时构建入口：`./script/build_and_run.sh --verify`。该命令不会启动 GUI、更新现有安装或滚动正式版与回滚版。
