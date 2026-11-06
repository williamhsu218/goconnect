# 菜单栏图标与可选速率 · 2026-11-06

本轮完成源码及隔离原生 App 验收，正式安装和发布独立执行。候选源码沿用现有版本号，下一次正式发布须按 [RELEASING.md](RELEASING.md) 升级版本与 build。

## 行为

- 菜单栏改为与应用图标呼应的路径 G／端点矢量，透明模板图像随系统菜单栏着色；保留未连接、进行中、已连接及注意状态徽章。Dock 图标保持现有应用图标。
- 设置 → 菜单栏增加“显示网络速率”，默认关闭。开启后“显示方向”可选“下行与上行”“仅下行”“仅上行”，默认双向；关闭时保留所选方向，恢复仅图标。
- 速率来自当前 GoConnect 线路已有采样，不是整台 Mac 网卡总量。单位为字节／秒，按 1024 进位，与连接页口径一致。
- 仅实际连接中的新鲜样本显示速率；需要重启部分应用的连接仍可显示。断开、恢复中、诊断、样本缺失或达到 5 秒过期时显示“—”；有效零值显示 `0 B/s`，非法方向数值不冒充零。
- 使用原生 `NSStatusBarButton`、等宽数字和固定槽位，双向 192pt，单向 109pt；关闭恢复系统方形宽度（本机 22pt）。工具提示和辅助功能包含所选方向语义。无额外网络查询；开启时按现有采样更新，并用一次性定时器处理过期。
- UI 偏好使用独立 `UserDefaults` 键 `menuBarShowsNetworkRates`、`menuBarRateDirection`，不改线路／订阅配置和流量数据库格式。

## 实现与验收

入口：[MenuBarSettings.swift](../Sources/GoConnect/Views/MenuBarSettings.swift)、[MenuBarController.swift](../Sources/GoConnect/App/MenuBarController.swift)、[AppBranding.swift](../Sources/GoConnect/App/AppBranding.swift)、[速率显示策略](../Sources/GoConnectCore/MenuBarRatePresentation.swift)。

- 最终全量 Swift **134 项通过**，含本轮 8 项速率策略回归，覆盖默认隐藏、方向选择、连接状态、采样过期、未来时间、零值与非法值、单位及极值上限。
- 最终 `./script/build_and_run.sh --build-only` 生产编译、完整打包、严格签名及脚本内运行组件检查通过，临时输出自动清除。本轮未重跑不受显示改动影响的 Go race／实链路门禁。
- 隔离 App 使用与源码相同的菜单栏控制器、图标和设置组件，网络／凭据实现未编入，仅注入合成状态。实际点击开关和方向选择后，原生按钮 title、尺寸、辅助功能、模板标志和窗口附着均读回符合预期，文本未越过按钮槽位。
- 冷启动未设置偏好时默认关闭；开启后正常退出再启动仍开启。方向选择由独立 `defaults` 进程读回，关闭不丢失所选方向。
- 停止合成采样并等待 6 秒后，实际原生按钮转为 `↓ —  ↑ —`；真实零和极大值均显示正确且不改变双向宽度。仅下行与仅上行分别读回对应标题和 109pt 槽位。
- 实际检查浅色／深色设置窗口和模板图标；本机实际菜单栏为 `NSAppearanceNameVibrantDark`。未改变系统外观；其它系统菜单栏外观及所有刘海屏／菜单项拥挤组合未逐机覆盖。

可复用预览入口：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 script/preview_menubar.py --run \
  --evidence /absolute/private-menubar-evidence
```

不带 `--run` 仅构建校验并清除临时 App。每个 evidence 路径对应独立 preview Bundle ID 和偏好域；同路径可用于重启持久化验收，结束后清除该 preview 域，勿删除正式 App 偏好。预览中的“当前原生菜单栏按钮”由实际按钮缓存绘制，背景按其原生 appearance 显示，未用静态假图替代。

## 跨 Agent 与证据

Agy 按用户指定 `gemini-3.8-flash-high / high` 提供基础矢量图标代码和有界实现审查；Codex 实现设置、方向选择、显示策略、Observation／偏好桥接及过期处理，完成集成、测试、打包与原生 UI 验收。调用成功，conversation `ccf025fc-b332-4608-97a6-9e0f923a1a7c`；型号与强度仅确认请求已固定，平台没有独立执行回执。未调用 Claude。

私有证据：`/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-menubar-115615/`。主要记录为 `swift-direction-tests.log`、`package-direction-build.log`、`native-live.json`、`native-stale.json`、`native-zero.json`、`native-extreme.json`、`native-download-only.json`、`native-upload-only.json`、`native-off.json`、`preview/session.json` 和 `coordination.json`。截图为 `download-only.jpg` 与 `upload-only-dark.jpg`；它们使用合成速率，不代表真实线路测速。

本轮正式 App、用户数据、进程及原有 Git 索引保护结果见 `final-readback.json`。隔离 App、临时构建和偏好清理见 `cleanup.json`。
