# GoConnect 前端设计升级评估

2026年11月5日。针对 macOS 27 的 Liquid Glass 方向，建议在现有原生界面上做增量升级，优先重排连接页、梳理工具栏分组和统一内容层次。推荐的视觉方向是**精密网络仪表**：用清晰的状态、精确的数据和克制的材质体现科技感。

本次只完成评估，未实施界面升级。应用源码、安装版、分发包与正在运行的 VPN 均未由本轮改动。

## 评估依据

- 实际调用 Claude Code 2.1.312，指定 `--model claude-opus-5-5 --effort xhigh`，同时以进程环境变量固定 `xhigh`。返回的 canonicalModel 为 `claude-opus-5-5`，provider 为 `firstParty`；调用成功，无权限拒绝。
- Claude 仅获得 `Read`、`Glob`、`Grep`，在 plan 模式运行；实际读取本轮五张截图和相关源码。未委派子代理。
- 当前安装版为 GoConnect 0.11.16 build 56；实机 macOS 27.2，本机 SDK 27.2。已安装主程序的 Mach-O SDK 标记为 27.0。
- 五张截图来自当前安装版，1880×1384 像素、2倍比例，约940×692点；均为浅色、已连接状态。只浏览页面，最后返回原来的订阅管理页。
- Apple 官方资料支持导航与操作层使用 Liquid Glass、内容层保持清晰稳定。本文的视觉名称、布局和参数属于设计提案，不是 Apple 的 macOS 27 专属规范。

## 逐页结论

| 步骤 | 页面与总体状态 | 本轮观察 | 建议 |
| --- | --- | --- | --- |
| 01 | 连接：主要优化对象 | 开关醒目；线路名在工具栏、状态说明及底部重复；已连接仍出现“开启 VPN”的引导；分流摘要落在首屏下方 | 合并线路区，减少重复标题；提高状态与实时数据的优先级；压缩套餐区域 |
| 02 | 线路配置：结构清楚，空间偏紧 | 全局侧栏、固定260点的线路列表与表单共用窗口；当前选中线路在列表可视区之外；只读说明明确 | 进入页面定位选中行；压缩列表行；评估可调宽分栏，保留 grouped Form |
| 03 | 订阅管理：原生基础良好 | 当前使用标识清楚，三条订阅后留白较多；工具栏并列多个图标操作 | 保留原生列表；整合导入入口；有真实总量时可增加简洁配额摘要，留白本身不算缺陷 |
| 04 | 应用白名单：可读，次要信息较多 | 原生筛选和列表已存在；每行同时用开关和文字说明是否加入；连接时只读有说明 | 精简重复文字，统一路径和标识符呈现；保持原生筛选、搜索与只读保护 |
| 05 | 诊断：信息充分，首屏偏重维护 | 服务说明与禁用维护操作占较多空间；下方环境、隧道检测两列密度不同 | 优先呈现环境摘要，将维护细节收拢；连接状态、授权状态与网络可达性分开表达 |

## 推荐视觉方向

**材质分层。** 保留原生 `NavigationSplitView`、侧栏、工具栏、sheet 和 popover。现有 Toast 的玻璃及减少透明度分支可以继续使用。数据区使用系统语义背景、细分隔和留白，只在确实需要分组时增加轻量表面。无需给每张卡片增加 `glassEffect`；现有单个自定义玻璃反馈也不需要为了形式加入全局 `GlassEffectContainer`。[Apple Materials](https://developer.apple.com/design/human-interface-guidelines/materials)

**色彩。** 继续尊重系统强调色；蓝或用户所选强调色承担选中与操作，绿表示已连接，橙表示需要处理或数据陈旧，红表示失败或破坏性操作。石墨灰、冷白与系统深色背景构成底色。主操作数量、图标密度和层次比增加彩色光晕更值得优先调整。

**文字与数字。** 建议让“连接状态 → 实时速率 → 累计与套餐信息”形成明确层级。当前速率已经采用等宽字体，升级应调整字号、单位和对齐，而不是把它描述为缺失能力。数值可试用系统字体配合 `monospacedDigit()`；IP、CLI 路径和技术标识继续使用等宽字体。保留24点页面留白和20点分组间距作为起点，具体尺寸由最小窗口渲染决定。

**动效。** 稳态以静止为主；连接、恢复、失败等阶段变化可使用短暂符号过渡。每秒更新的数据避免布局抖动。沿用现有减少动态与透明度分支，新增呈现也需要覆盖这两种设置。

**科技感来源。** 强化真实的连接状态、数据时效、出口检测时间、分流范围和未知项。历史速率曲线可作为后期功能，但需要新增会话采样、缺失值语义和可访问摘要；没有数据时显示缺口，不能用装饰波形代替。

## 连接页目标布局

```text
原生工具栏：侧栏切换 / 页面标题 / 当前页面所需操作

状态符号  已连接                           VPN 连接 [原生开关]
          线路名称 · 分流模式                     [线路设置]
          按状态显示锁定、恢复或错误说明

下行速率                 上行速率                 连接时长
累计接收                 累计发送                 会话说明

公网出口 IP · 检测时间 [刷新]
隧道线路才展示虚拟地址；窄窗口允许换行

套餐摘要：剩余 / 已用与总量 / 更新时间 / 手动刷新
分流摘要：模式 / 名单数 / 直连规则数 / 管理入口
```

主卡只保留一个线路位置：未连接时放原生线路选择器，连接或忙碌时放静态线路名称及状态说明。保留原生 VPN 开关、可见文字、诊断停止入口及完整的恢复提示。代理线路不再占用一个固定的“不适用”指标格；隧道线路继续显示地址缺失、多地址及帮助信息。

工具栏继续提供原生导航和页面操作。连接页可以隐藏重复的“查看连接状态”整项，其余页面保留回到连接页的入口。按“连接摘要、页面操作、搜索”组织功能；不同页面拥有不同操作数量时，允许系统正常调整位置。

## 分期与实施位置

P1表示首期设计工作，不表示严重软件缺陷。S为小，M为中，L为大；均为相对工作量。

| 优先级 | 建议与依据 | 实施位置 | 工作量 | 主要验收标准 |
| --- | --- | --- | --- | --- |
| P1 | 重排连接页，合并重复线路区，按连接状态调整引导文案；见01 | [ConnectionView.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/ConnectionView.swift:10)、[VPNConnectionCard.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/VPNConnectionCard.swift:59) | M | 主卡线路名仅一处；开关、恢复与诊断说明完整；首屏分流入口更易发现 |
| P1 | 固定三个主要指标，按线路类型展示地址；见01 | [VPNConnectionCard.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/VPNConnectionCard.swift:192) | S–M | 代理线路不出现“不适用”占位；隧道地址状态准确；指标位置稳定 |
| P1 | 优化工具栏功能分组和长名称呈现；见01至05 | [ContentView.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/ContentView.swift:75) | M | 状态与新增/导入操作可区分；最小宽度及展开搜索时不重叠、不丢失入口 |
| P1 | 定位线路列表当前选中项，改善列表与表单的宽度分配；见02 | [ProfilesView.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/ProfilesView.swift:30) | M | 进入页面选中行可见；键盘导航、长名称和草稿行均正常 |
| P2 | 统一内容边界、对齐、数字层级与次要操作；见01、03、05 | [DesignSystem.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/DesignSystem.swift:74)、[VPNConnectionCard.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/VPNConnectionCard.swift:233) | M | 浅深色均清晰；增强对比可辨；次要操作不会争夺主要状态的注意力 |
| P2 | 统一套餐卡、只读提示和陈旧数据样式；见01、02、04、05 | [SubscriptionUsageCard.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/SubscriptionUsageCard.swift:13)、[YunyooUsageCard.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/YunyooUsageCard.swift:1) | M | 仍仅手动刷新；授权失效保留旧值和时间；连接期间配置保护保持有效 |
| P2 | 精简应用副文本和诊断维护区，评估符号语义；见04、05 | [ApplicationsView.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/ApplicationsView.swift:110)、[DiagnosticsView.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Views/DiagnosticsView.swift:39) | M | 配置名单、实际路由、授权与可达性不混淆；技术详情仍可访问 |
| P3 | 真实会话速率曲线、分流比例、分层应用图标 | [SessionTrafficMeter.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnectCore/SessionTrafficMeter.swift:1)、[AppStore.swift](/Users/willhsu/Creations/GoConnect/Sources/GoConnect/Stores/AppStore.swift:408)、[make_icon.swift](/Users/willhsu/Creations/GoConnect/script/make_icon.swift:1) | M–L | 作为新增功能单独确定范围；数据缺口准确；图标打包与辅助功能通过验证 |

建议先完成P1形成隔离预览，再决定P2的具体材质与色彩参数。P3包含新增产品能力，不应被默认并入纯视觉升级。

## 对 Claude 原始结论的核对

1. **原生蓝色不是已证实的缺陷。** 根级 `tint` 可能影响局部表现，但目前没有运行实验确认因果。可以对次要操作做局部比较，不建议全局移除强调色。
2. **工具栏横向位置变化是观察事实，是否造成使用困难仍需验证。** 原始报告的约243点位移、约130点节省空间都是截图估算。验收应关注功能分组、可发现性与无溢出，不要求所有页面绝对固定在同一横坐标。
3. **原生 `glassProminent` 不应一律替换。** 是否适合取决于按钮层级和上下文；原始报告提出的统一替换属于风格选择，不是本轮证实的系统规范违规。
4. **列表留白、系统滚动边缘和侧栏透明观感不直接定性为错误。** 需要结合壁纸、滚动状态、激活状态和系统外观比较，优先保留系统结构。
5. **“科技感”与符号解释包含设计判断。** 例如路由计数的盾牌可能带来保护联想，但本次没有用户研究证明误读；可在下一轮方案中采用更中性的路由符号。
6. **旧系统支持需要单独验收。** 源码部署声明为macOS 14，安装版 Info.plist 的最低系统值为26.0，主程序 Mach-O 的 minos 为14.0；不能据此宣称安装版已支持macOS 14。新 API 的可用性应以实际 SDK 和构建检查为准。

## 实施后的验收范围

以下为计划，尚未作为升级结果实测。

| 场景 | 验收重点 |
| --- | --- |
| 940×640最小窗口、980×700默认窗口 | 开关、状态、指标与主要入口完整；工具栏及搜索不溢出 |
| 长中文、英文线路名 | 合理折行或省略；help和辅助功能保留完整名称 |
| 14种连接控制状态 | 文案、符号、开关可用性、指标占位与恢复提示正确 |
| 浅色、深色、增加对比度 | 表面、次要文字、禁用控件和进度条清晰；对比度需要实测 |
| 减少透明度、减少动态效果 | 玻璃降级和状态过渡符合设置；稳态无持续装饰动画 |
| 键盘、VoiceOver | 焦点与列表操作正常；开关不重复朗读；数据与时间信息完整 |
| 空数据、错误、授权失效 | 不把未知显示成成功或零；保留旧数据并标明时效；长错误可访问 |
| 运行与打包 | 使用项目外scratch及隔离预览；验证SDK标记；按项目门禁处理构建、测试与清理 |

本轮没有观察深色、断开、连接中、恢复中、空列表、错误、设置、弹窗及真实辅助功能设置下的界面。截图能支持视觉观察，不能证明完整交互、可访问性或网络功能验收通过。

## 本轮截图

### 01 连接

开关和实时数据可辨，首屏信息层级及重复线路区值得优先调整。

![01 当前连接页](/Users/willhsu/.codex/visualizations/2026/11/05/01a1ab2b-3e90-70f1-908a-1a5f2751506c/goconnect-design-audit/01-connection.png)

### 02 线路配置

表单分组清楚，当前选中线路位于列表可视区下方。

![02 当前线路配置页](/Users/willhsu/.codex/visualizations/2026/11/05/01a1ab2b-3e90-70f1-908a-1a5f2751506c/goconnect-design-audit/02-profiles.png)

### 03 订阅管理

当前使用状态明确，可精简工具栏入口。

![03 当前订阅管理页](/Users/willhsu/.codex/visualizations/2026/11/05/01a1ab2b-3e90-70f1-908a-1a5f2751506c/goconnect-design-audit/03-subscriptions.png)

### 04 应用白名单

原生列表与筛选结构可保留，副文本和状态表达可简化。

![04 当前应用白名单页](/Users/willhsu/.codex/visualizations/2026/11/05/01a1ab2b-3e90-70f1-908a-1a5f2751506c/goconnect-design-audit/04-applications.png)

### 05 诊断

服务状态和只读说明明确，维护详情占用较多首屏空间。

![05 当前诊断页](/Users/willhsu/.codex/visualizations/2026/11/05/01a1ab2b-3e90-70f1-908a-1a5f2751506c/goconnect-design-audit/05-diagnostics.png)

## 原始评估与官方依据

- [Claude 原始评估](/Users/willhsu/.codex/visualizations/2026/11/05/01a1ab2b-3e90-70f1-908a-1a5f2751506c/goconnect-design-audit/claude-assessment.md)，保留原始意见，与本文件的核对结论区分。
- [调用参数记录](/Users/willhsu/.codex/visualizations/2026/11/05/01a1ab2b-3e90-70f1-908a-1a5f2751506c/goconnect-design-audit/invocation.json)与[模型及文件核对结果](/Users/willhsu/.codex/visualizations/2026/11/05/01a1ab2b-3e90-70f1-908a-1a5f2751506c/goconnect-design-audit/verification.json)。原始工具记录及截图保存在同一项目外证据目录。
- [Apple Adopting Liquid Glass](https://developer.apple.com/documentation/technologyoverviews/adopting-liquid-glass)。
- [Apple Materials](https://developer.apple.com/design/human-interface-guidelines/materials)。
- [Claude Code 模型与推理档位配置](https://code.claude.com/docs/en/model-config)。
