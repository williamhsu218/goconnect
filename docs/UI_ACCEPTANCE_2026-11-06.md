# 浅色精密与按线路流量统计：隔离 UI 验收 · 2026-11-06

**本轮覆盖的候选界面与交互通过。** 已在本机原生隔离 App 中检查实际渲染并修复问题；没有安装、发布、替换正式 App 或重连 VPN。本文是工程验收记录，不代替用户对视觉方向的最终判断，也不代表真实线路流量或服务商计费对账通过。

## 本轮修复

- `TrafficHistoryView.swift`：分类轴显式绘制稀疏标签，保留完整小时文字和原有每日刻度间隔。Claude 提供初始修改，Codex 在实际窗口中发现并修正 `centered` 导致的一格偏移；最终分时、日、月、年及 31 天月份的标签均与对应柱体对齐。
- `ProfileTrafficStore.swift`：同一核心任一方向计数回退时，两个方向共同开始新计数周期，避免另一方向已超过旧数值时漏记。重复样本仍不增加字节，保存重读保持一致。
- `ProfileTraffic.swift`：年视图的范围按所选线路计算，避免新线路带入其他线路的历史年份；图表、明细与总量使用同一线路范围。
- `preview_ui.swift`：增加实际零值、缺失采样与未来空白的稀疏合成场景；避免初始场景重复注入，持续记录布局稳定后的窗口外框及内容尺寸。仅属于预览工具，不进入产品 target。

Swift Charts 的 `centered` 参数表示标签是否放在两个轴值之间，并不是通用的文字居中开关；分类柱图最终使用 `false`。[Apple AxisValueLabel 参数说明](https://developer.apple.com/documentation/charts/axisvaluelabel/init(centered:anchor:multilabelalignment:collisionresolution:offsetsmarks:orientation:horizontalspacing:verticalspacing:content:))

## 实际验收范围

| 检查 | 证据与结果 |
| --- | --- |
| 原生窗口 | 默认外框 980×700；最小内容区 940×640，对应含标题栏外框 940×692。原生侧栏收起、恢复均有效，未修改系统显示设置。 |
| 连接页 | 浅色、深色、长线路名、完整 IPv6、AnyConnect 多地址；卡片无明显溢出，出口 IP 与隧道虚拟地址含义分别保留。 |
| 控制状态 | 14 种注入状态的标题、开关开闭及可操作性均符合 `VPNControlState`；这是控件渲染/AX 检查，没有实际连接或断开网络。过渡截图不作为开关动画结束后的状态证明。 |
| 流量统计 | 实际点击分时/日/月/年、前一时段、返回当前时段；检查 31 天月份、上下行合计、图例及明细滚动。 |
| 记录语义 | `00:00` 实测 `0 B`、`01:00` 未记录、`02:00` 有流量；其余已过时段灰带，未来时段无柱且不出现在明细中。浅深色均检查。 |
| 线路选择 | 选择其他统计线路不切换连接；无历史线路显示“暂无记录”；已删除线路仍可选择，并有明确归档说明。 |
| 线路配置 | 当前连接只读、其它线路可编辑；新草稿完整呈现，未保存草稿暂不能查看统计。继续编辑保留输入，放弃更改后进入目标页。 |
| 保存与读回 | 在临时数据目录实际保存合成线路，从 18 条变为 19 条；磁盘读回名称、服务器符合输入，当前连接仍为 Tokyo Japan。 |
| 订阅与节点 | 列表、进入节点及返回；添加订阅初始名称焦点与 Escape 取消；页面未挂载套餐余量或服务商授权入口。没有更新真实订阅或进行测速。 |
| 其它界面 | 应用白名单、直连 App、诊断、原生设置窗口；连接期间配置只读状态与提示一致。 |
| 长错误 | 注入长错误，内部滚动值实际变化，关闭提示后主界面保留。 |
| 辅助功能分支 | 注入增加对比度、减少透明度和减少动态效果，检查自定义表面及文字；未更改真实系统设置。 |

对日期早于首次记录的浏览，保留“这段时间暂无记录”及返回当前时段的操作，以便不同线路比较同一日期；不擅自替用户改变所选时段。历史名称取最后一次采样记录的名称，UUID 归属不变。

## 构建与保护

- 两项新增回归先在原实现上失败，修复后全量 **112 项 Swift 测试通过**。日志 `regression-before.log`、`swift-final-tests.log`。
- 对最终图表代码执行 `./script/build_and_run.sh --build-only`，生产构建、打包与脚本内组件检查通过；临时输出自动清理。未生成保留的候选 ZIP。
- 隔离 App 编译及严格签名检查通过，Bundle ID `com.willhsu.GoConnect.UIPreview`。独立合成数据、替身钥匙串、非持久 WebKit，不含 Runtime，特权授权路径阻断。最终 21 个视图文件指纹与仓库一致。
- `release_artifacts.py check` 与 `git diff --check` 通过。留存 current 0.11.14/build 51、rollback 0.11.13/build 50 未滚动。
- 结束读回：**139 个正式 App、用户数据和分发文件指纹未变；7 个原进程的 PID、启动时间与路径未变。** 安装版仍为 0.11.16/build 56。
- 测试 App 已退出，临时 App、数据、Swift scratch 与审查副本已清理；证据记录中的临时 App 路径不再有效。

## 跨 Agent 执行

用户本轮指定 Claude Opus 5.5/high、Agy Gemini 4 Argon/medium；限额时可由其余已指定 Agent 或 Codex 接手。本轮没有触发限额换手。

- Claude Code 实际执行图表修改及最终 6 张截图的只读复审。两次调用的 `modelUsage.canonicalModel` 均为 `claude-opus-5-5`，无权限拒绝；high 在参数与进程环境中固定，平台没有独立 effort 回执。
- Agy 的 `gemini-4-argon-medium` 来自实时模型清单，调用显式传 medium。首轮因只读工具权限阻断无可用结论；保持模型和权限，以提供的源码完成一次有界纠正审查。模型选择的返回证据仅为会话元数据，没有底层 API 模型/强度独立回执。
- Codex 实际承担桌面交互、问题复现、统计修复、回归测试、图表纠偏与集成、构建、正式环境保护核对及清理。模型意见未代替实际验证。

## 通过声明的限制

没有在测试 App 中加载真实 VPN Runtime，因此未验证新统计在真实线路上的采集/重启对账；正式 App 仍运行原版本。夏令时 23/25 小时、半小时时区由核心测试覆盖，本轮没有对应时区的原生图形截图。未进行 VoiceOver、旧版 macOS 或真实系统辅助功能设置的全面验收。

系统弃稿确认框在当前截图接口中返回空白图，因此其文案和操作只由 AX、继续编辑及弃稿后的状态验证；该空白图不作为视觉通过证据。原生添加订阅和设置窗口有真实渲染截图。刻度线与部分标签的细微接触、低占比上传色块不易分辨为非阻塞观察，具体值可从完整明细读取。

## 证据位置

[本轮证据目录](/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-ui-acceptance-084954)

- `accepted-connection-light.jpg`、`connection-dark-long-ipv6.jpg`：连接页。
- `accepted-traffic-hourly-minimum.jpg`、`accepted-traffic-daily.jpg`、`accepted-traffic-31-days.jpg`、`accepted-traffic-monthly.jpg`、`accepted-traffic-yearly.jpg`：最终图表。
- `accepted-traffic-zero-unknown.jpg`、`accepted-traffic-dark.jpg`、`accepted-traffic-archived.jpg`、`accepted-traffic-long-name.jpg`：数据和布局分支。
- `control-matrix.json`、各页 `.ax.txt`、`profile-save-readback.json`：状态及交互证据。
- `claude-ui/summary.json`、`claude-final-review/answer.txt`、`agy-correction-result.json`、`review-dispositions.json`：调用与独立审查。
- `accepted-package-build.log`、`preservation-readback.json`、`acceptance-summary.json`：构建、保护与收尾读回。
