# 浅色精密前端重构 · 2026-11-05

**最新状态（2026-11-06）：本轮隔离 App UI 验收通过，未安装或发布。** 修复、实际覆盖范围和限制见[隔离 UI 验收记录](UI_ACCEPTANCE_2026-11-06.md)。下文保留 11-05 的历史过程。用户选定原会话 `01a1ab2b-3e90-70f1-908a-1a5f2751506c` 的第一张美术稿；没有替换当前运行的 GoConnect。

后续按用户新决定，当前候选已将套餐余量替换为本机按线路统计，新增分时、日、月、年记录；实现说明见[按线路记录本机流量](PROFILE_TRAFFIC_HISTORY_2026-11-05.md)。本文下方是解锁前的 UI 重构阶段记录，11-06 验收进展以上述最新记录为准。

最终桌面检查时 Mac 已锁屏。用户随后明确选择“暂时无法解锁，先完成代码与构建”。因此关闭并清理临时预览，保留源码、日志和第一轮交互证据，解锁后继续最终渲染验收。

## 协作与设计约束

- 实际调用 Claude Code 2.1.312，指定 `claude-opus-5-5 --effort xhigh`，并固定进程环境中的 `CLAUDE_CODE_EFFORT_LEVEL=xhigh`。调用成功，返回的 canonicalModel 为 `claude-opus-5-5`、provider 为 `firstParty`，无权限拒绝。
- Claude 仅获得 `Read/Glob/Grep/Edit/Write`，负责五个文件：DesignSystem、VPNConnectionCard、ConnectionView、ApplicationRoutingCard、SubscriptionUsageCard。禁止 Bash、MCP、hooks、构建、安装和网络操作；实际编辑范围与分工一致。
- Codex 负责整体范围、源码语义审查、导航及其他页面、隔离测试 App、完整构建检查与正式状态保护读回。双方未使用额外子代理。
- 保留原生 `NavigationSplitView`、工具栏、系统开关、表单、弹窗和 popover。玻璃集中在导航与操作层，数据内容保持稳定可读；不增加图表、装饰波形或安全保障结论。
- 遵循系统强调色及浅深色设置，不强制白色背景。自定义冷色渐变在增加对比度、减少透明度时关闭；保留原有减少动态效果分支。

依据为 [Apple Materials](https://developer.apple.com/design/human-interface-guidelines/materials) 与 [Adopting Liquid Glass](https://developer.apple.com/documentation/technologyoverviews/adopting-liquid-glass)。这些设计选择及构建检查不构成完整可访问性或旧系统兼容性认证。

## 源码变化

| 范围 | 最终实现 |
| --- | --- |
| 连接页 | 删除重复大标题、工具栏线路及状态快捷入口；线路仅在主状态区显示一次。保留原生开关、线路设置、诊断停止和恢复说明。 |
| 连接指标 | 下行、上行、时长三列开放排版；26pt 主数字、较小单位、等宽数字及细分隔。保留 5 秒数据时效，未连接的保留累计明确标为上次会话。 |
| 地址 | 公网出口 IP 与 AnyConnect 隧道虚拟地址分别展示；代理线路不再显示“不适用”指标格。保留多地址帮助、IP 刷新与检测时间，并提供长值换行布局。 |
| 套餐与分流 | 摘要在宽度足够时左右分栏，不足时堆叠；一张可见摘要时占满行。套餐保留溢出安全计算、手动刷新、授权同步和陈旧提示；缺少更新时间也不隐藏授权失效说明。 |
| 共享视觉组件 | Surface 圆角 14、内边距 18、克制边界；浮动提示保留 18 圆角。新增只读数字、分组标题、说明及连接页背景组件。 |
| 线路配置 | 原生 `HSplitView` 可调分栏，进入页面自动定位选中线路。保留新增草稿 List 重建修复、未保存保护和当前线路只读规则。 |
| 订阅管理 | YAML 与 Clash Verge 导入合并到原生导入菜单，添加订阅仍为独立操作。 |
| 应用名单 | 精简每行与开关重复的加入状态文字，保留系统开关的状态及可访问名称、路径帮助和上下文菜单。 |
| 诊断 | 本机服务维护详情默认收拢，保留状态摘要及全部维护操作。分流计数使用中性的网络符号。 |
| 云悠流量 | 操作与用量在窄区域自适应堆叠，数字统一使用系统字体及等宽数字，原登录与手动刷新行为保留。 |

相对于开始时记录的文件指纹，本轮只改变 11 个既有视图文件；既有 Stores、Services、GoConnectCore、Transport、Tests、版本和构建脚本未由本轮改动。仓库开始时已有的未提交工作保留。

新增维护入口为 [preview_ui.py](../script/preview_ui.py) 和 [preview_ui.swift](../script/preview_ui.swift)。它们只在项目外临时副本中编译测试 App，不进入生产 target。

## 已完成验证

| 检查 | 结果与范围 |
| --- | --- |
| 完整构建入口 | `GOCONNECT_TEST_TRANSPARENT=0 GOCONNECT_TEST_SHARED_COMPANY=0 PYTHONDONTWRITEBYTECODE=1 ./script/build_and_run.sh --verify` 成功。没有运行正式 GUI 或操作安装服务。 |
| Swift | 91 项测试，0 失败。 |
| Go、打包与运行组件 | Go race、包内组件及动态依赖、严格 deep 签名与隔离集成检查通过。安装服务/TUN 测试保持显式关闭；受权限限制的测试保留其跳过状态，不视为真实路由验收。 |
| 测试 App | 独立 `.app` 已编译并启动，Bundle ID 为 `com.willhsu.GoConnect.UIPreview`，签名检查通过，Mach-O SDK 为 27.2。最终预览的 19 个视图源文件指纹与仓库一致。 |
| 预览隔离 | 独立合成数据目录、不含 Runtime、钥匙串替身、非持久 WebKit 存储，特权授权路径在临时副本内阻断；隔离变换不匹配时拒绝构建。构建与 App 均在系统临时目录，退出后清理。 |
| 第一轮真实窗口交互 | 当前线路自动定位、非当前线路可编辑、合成配置保存及磁盘读回、新草稿完整呈现、继续编辑保留草稿、弃稿后进入目标页、订阅节点进入及返回、添加订阅名称焦点、Escape 取消均已验证。该轮发生在共享视觉组件定稿前，不能替代最终整体渲染复核。 |
| 正式环境保护 | 正式安装版仍为 0.11.16 / build 56；120 个受保护文件指纹未变，7 个原 GUI/服务/网络进程的 PID、启动时间与路径保持相同。未重连 VPN、修改路由或用户配置。 |
| 分发包 | `release_artifacts.py check` 通过；既有 current 0.11.14 / build 51、rollback 0.11.13 / build 50 未滚动。安装版与留存 ZIP 是不同边界。 |
| 源码及清理 | `git diff --check` 通过；本轮临时预览、Swift scratch、包和运行组件已由对应脚本清理，不保留候选 ZIP。 |

## 11-05 记录的后续验收清单

以下是当时未完成的清单；11-06 已完成的覆盖与仍未覆盖的边界见最新验收记录，不能仅据旧构建成功或第一轮截图宣称通过：

1. 最终连接页在 980×700、940×640、侧栏不同宽度下的实际布局；三指标分隔线、摘要等高及首屏入口。
2. 14 种连接控制状态、AnyConnect 多地址、长线路名及 IPv6、无套餐数据和云悠流量分支。测试工具支持在临时副本内注入控制状态，不修改生产状态逻辑。
3. 浅色、深色及自定义增加对比度/减少透明度/减少动态效果分支。注入分支不是实际系统设置或 VoiceOver 认证。
4. 最终视觉组件对线路、订阅、节点、名单、诊断、设置和弹窗的影响；长错误滚动与关闭、键盘及可访问名称。
5. 由 Claude 读取最终真实截图复审，Codex 核对并修复实际发现的问题，再给出视觉验收结论。整个过程仍限隔离 App，不替换正式运行版。

可重新启动：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 script/preview_ui.py --run \
  --evidence /Users/willhsu/.codex/visualizations/2026/11/05/goconnect-light-precision-refactor/resumed
```

预览目录记录在指定 evidence 的 `preview-session.json`。`preview-command.json` 可以选择合成 `scenario`、`page`、`theme`、窗口宽高和测试辅助功能分支；仅供维护验收使用。关闭测试 App 后其临时应用和数据目录自动移除。

## 证据

项目外目录：[/Users/willhsu/.codex/visualizations/2026/11/05/goconnect-light-precision-refactor](/Users/willhsu/.codex/visualizations/2026/11/05/goconnect-light-precision-refactor)。

- `claude-implementation-invocation.json`、`claude-implementation-result.json/.md`：实际模型、档位、权限、编辑轨迹和实现说明。
- `verification-build.log`：完整构建、91 项 Swift、Go race、签名和隔离集成日志。
- `preview-build.log`、`preview-session.json`、`preview-sdk.txt`：最终候选测试 App 的编译、源文件对应及 SDK 证据。记录中的 App 路径在清理后失效，须重新运行预览。
- `interaction-acceptance.json`、`after-profiles.png`、`after-new-draft.png`、`after-subscriptions.png`、`after-nodes.png`、`after-diagnostics.png`：第一轮交互证据，不是最终连接页的视觉验收截图。
- `baseline.json`、`preservation-readback.json`、`final-verification.json`：开始基线、正式环境保护和最终清理读回。
