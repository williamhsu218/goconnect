# 按线路记录本机流量 · 2026-11-05

**最新状态（2026-11-06）：隔离 UI 验收及修复后的 112 项 Swift 测试、生产构建通过；未安装或发布，未验证真实线路流量对账。** 详见[本轮 UI 验收记录](UI_ACCEPTANCE_2026-11-06.md)。下文保留 11-05 的实现与构建过程；没有替换当前运行的 GoConnect，没有操作 VPN、VPS 或正式分发包。

用户参考会话 `01a19123-18fa-7a60-82bc-9a688d0593f1` 中的 Cookie 到期问题，要求移除订阅套餐余量展示，改为每条线路的本机流量统计。旧方案依赖服务商认证；新候选的统计与展示均不需要服务商登录。

## 界面与范围

- 连接页用“当前线路流量”替换套餐卡，显示今日、本月已记录的合计消耗及下载/上传，提供“查看统计”。
- 侧栏新增“流量统计”。线路选择器仅选择统计对象，不切换实际 VPN；线路配置页也有当前查看线路的统计入口，新建未保存的草稿暂不提供该入口。原有未保存确认仍有效。
- 粒度为“分时 / 日 / 月 / 年”：分别按所选日的小时、所选月的日期、所选年的月份、全部历史年份展示。自然月、自然年按统计时区计算，不采用服务商账单周期。
- 图表使用系统 Swift Charts，上下行堆叠，配套数值明细。没有采样的时段显示“未记录”，只有实际收到零计数的时段才显示 `0 B`；未来时段不补造流量。
- 订阅列表、节点页、连接页移除套餐余量、云悠登录与重新授权入口。节点导入、更新、测速、线路选择继续使用原流程。
- 旧套餐类型及其兼容测试暂留，原数据不删除。旧云悠模块改为按需初始化，普通启动不加载其认证缓存；新挂载界面不访问它。原 VPS 查询服务和配置保持原状。

Claude Code 以 `claude-opus-5-5 --effort xhigh` 实际完成两个新视图及四个现有视图的改造；返回 canonicalModel 为 `claude-opus-5-5`、provider 为 `firstParty`，调用成功。Codex 负责总体设计约束、统计模型、存储、生命周期接入、源码复核、测试及构建。未调用额外子代理，未运行桌面自动化。

## 统计口径与持久化

统计按保存线路的 UUID 归属。重命名不会分裂历史；复制线路得到新 UUID，独立计数；删除配置不删除已记录历史，统计页可查看已删除线路。

数据来自 GoConnect 网络组件报告的累计上下行计数，以增量累计，重复上报不重复加字节，核心计数归零后从新计数继续累计。HY2 使用节点核心的计数，不合入系统路由核心的 DIRECT 流量。诊断测试、没有实际会话及会话结束后的迟到事件不记入历史。暂时没有计数上报时保留上一次累计基线，恢复上报后的差额仍会计入。

这是**本机经 GoConnect 上报的用量**，不是 VPS 或服务商套餐余额：不包含其他设备、其他客户端及启用此版本前的使用历史，协议开销与服务商计费也可能不同。按采样上报所在小时归档；跨时段的采样间隙不能精确还原每个字节的发送时刻。连接断开前最后一次上报之后的流量也可能未被采到。

本地独立文件为数据目录中的 `traffic-history.sqlite`，不改现有 `configuration.json` schema。采用系统 SQLite，按线路及小时更新少量行，保留完整小时历史，不按月清空。统计时区首次创建后保存在数据库，避免系统时区变化悄悄移动历史日期；支持夏令时 23/25 小时日。

每 5 秒提交待写变化，没有新采样时也会按时提交；断开、切换会话及正常退出时保存。写入失败保留内存和待写记录，稍后重试。突然断电/进程被强制终止，仍可能丢失最后一次提交后的约 5 秒记录，采样本身也不是服务商计费精度。

计数保留 UInt64 范围，SQLite 用十进制文本存储以避免有符号整数截断；加法饱和时标记上限，界面显示下界提示。事务写入，文件权限为 0600。损坏、未知 schema 和符号链接文件拒绝读取/写入，保留原文件，界面显示错误，不把未知当成零。

## 实现位置

| 文件 | 内容 |
| --- | --- |
| [ProfileTraffic.swift](../Sources/GoConnectCore/ProfileTraffic.swift) | 统计粒度、上下行合计、线路身份、时区和时间聚合；未记录与零的语义。 |
| [TrafficHistoryDatabase.swift](../Sources/GoConnect/Services/TrafficHistoryDatabase.swift) | SQLite schema、校验、事务、精确计数和受保护文件访问。 |
| [ProfileTrafficStore.swift](../Sources/GoConnect/Stores/ProfileTrafficStore.swift) | 会话增量、核心归零、持久化、定时保存与重试。 |
| [AppStore.swift](../Sources/GoConnect/Stores/AppStore.swift) | ready/stats/stop 接入、统计页导航；旧套餐模块按需初始化。 |
| [GoConnectApp.swift](../Sources/GoConnect/App/GoConnectApp.swift) | 退出时保存待写历史。 |
| [TrafficHistoryView.swift](../Sources/GoConnect/Views/TrafficHistoryView.swift) / [TrafficUsageCard.swift](../Sources/GoConnect/Views/TrafficUsageCard.swift) | 统计页与连接页摘要，原生 Charts、选择器、时段导航和明细。 |
| ContentView / ConnectionView / SubscriptionsView / ProfilesView | 导航、摘要替换、移除套餐展示及线路统计入口。 |
| [preview_ui.swift](../script/preview_ui.swift) | 明天隔离预览的合成流量样本及统计页场景；样本不在生产源文件中。 |

SQLite 链接到 macOS 系统库，没有下载或打包新的第三方数据库组件。技术依据：[Apple Swift Charts](https://developer.apple.com/documentation/charts)、[SQLite UPSERT](https://www.sqlite.org/lang_upsert.html)。

## 已验证与待验收

| 检查 | 结果 |
| --- | --- |
| 时间聚合 | 跨日/月/年、闰年、23/25 小时夏令时、半小时时区及系统时间后退测试通过。 |
| 流量与身份 | 首次累计、重复采样、计数归零、会话重启、线路切换归属、重命名、无记录与零、溢出测试通过。 |
| 存储 | 提交与重读、幂等提交、统计时区保持、完整 UInt64、损坏/未来版本保护、符号链接保护、写失败重试及无新采样时定时保存测试通过。 |
| AppStore | 诊断和迟到样本排除、计数暂不可用后继续累计；查看统计不切换线路、不绕过未保存确认，测试通过。 |
| Swift | 最终全量 110 项测试，0 失败；其中本轮新增 19 项功能测试。 |
| 构建及签名 | 完整 `--verify` 通过 Go race、包内组件、严格签名和隔离集成检查。随后针对最终 Swift 改动重跑全量 Swift 与 `--build-only`，均通过。 |
| 测试 App | 最终独立 Bundle ID 的预览构建通过；本轮未传 `--run`，没有启动测试 GUI。 |
| 正式状态与清理 | 见 `final-verification.json`：正式 App、用户配置、分发包和原运行进程保持不变，临时构建目录已清理。 |

安装服务/TUN 的显式测试没有启用，相关跳过项不视为实际线路验证。当前正式 App 仍运行旧版本，尚未开始记录本轮新功能的数据。

11-05 列出的后续验收重点包括：940×640 内容区与默认窗口，浅深色及辅助功能分支，连接页摘要与新工具栏空间，图例和明细可读性，分时/日/月/年及空记录，已删除线路、长名称、未保存草稿、重连与真实流量的保存/重开读回。11-06 已完成的 UI 覆盖和仍未覆盖的真实连接边界见最新验收记录；仍不替换正式版。

```sh
PYTHONDONTWRITEBYTECODE=1 python3 script/preview_ui.py --run \
  --evidence /Users/willhsu/.codex/visualizations/2026/11/05/goconnect-profile-traffic/resumed
```

默认合成场景包含跨日/月/年的数据；`scenario: empty` 可检查空历史，`page: traffic` 可直接进入统计页。测试 App 使用独立数据目录、替身钥匙串、非持久 WebKit，且不含真实 Runtime。

## 证据

[/Users/willhsu/.codex/visualizations/2026/11/05/goconnect-profile-traffic](/Users/willhsu/.codex/visualizations/2026/11/05/goconnect-profile-traffic)

- `claude-ui-invocation.json`、`claude-ui-result.json/.md`：实际模型、档位与界面实现说明。
- `core-tests.log`、`store-tests.log`、`swift-final-tests.log`：聚合、持久化与最终 110 项 Swift 测试。
- `verification-build.log`、`final-package.log`：完整检查及最终候选打包；候选未保留或安装。
- `preview-build-only/preview-build.log`：最终隔离测试 App 的构建检查。
- `baseline.json`、`final-verification.json`：正式环境保护与清理读回。
