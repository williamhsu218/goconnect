# GoConnect 1.0.4 菜单栏宽度修正与发布

日期：2026-11-06。正式版 1.0.4，build 61，stable。

- [正式 Release](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.4)，ID `436142012`，非 draft、非 prerelease，latest。
- 发布源码/tag：`d015598245f36dcdb991edbdbb01e80dd84781a4` / `v1.0.4`。
- 主构建代码提交：`bc1c0632a227b149b00de2e9f835e8d39d8e4473`。其后仅修正 README 两行（布局说明及 1.0.3 更新支持时间线）；所有编译、打包和网络组件输入未改变。完整差异与哈希见 `source-freeze.json`。
- 默认 main：`ce09a39a54b52a7ecab0c0b96f32f04061aaa3ea`，保留前 main 与发布源码历史，树与 tag 相同：`5083b817773a3f39c5ddaa3af32650942d254d78`。
- [固定签名更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)，updates 提交 `d7c62fa4b0f2fdb2f7f2899776af8dfa753fa8e9`；从 `e92f0626bfe8e62b124773ecba7b53bcf9fab516` 正常 fast-forward，未 force push。

## 修正与原生界面验收

用户反馈双向速率横向并排过宽。本版把图标与双行文字合成为单色模板，保留原生 NSStatusBarButton 的菜单、点击、高亮、辅助功能和系统着色。

- 双向从 192pt 收紧至 90pt，减少 53.125%；上下排列下行与上行。
- 单向居中显示一行，同为 90pt，切换方向不移动邻近菜单项；关闭后恢复本机 22pt 系统方形按钮，默认仍关闭。
- 等宽数字、完整 B/s／KB/s／MB/s／GB/s 单位、5 秒过期、真实零值和状态徽章不变。工具提示与辅助功能保留完整方向与数值。
- 原生按钮读回：宽 90pt，模板图像 85×22pt，已附着菜单栏；实际缓存绘制检查两行文本完整，包含 100.0 MB/s 和 1024 MB/s 的四舍五入边界。图像仅在状态或文字变化时更新，重建状态项时清除渲染缓存。
- 独立预览实际点击了双向／仅下行／仅上行、零值、极值、关闭和恢复菜单；停止样本后实际转为暂无数据且宽度不变。最终组件哈希与发布源码匹配。当前实际菜单栏为 VibrantDark；没有改变系统外观，也未逐机验证所有屏幕与缩放组合。
- 预览未编入网络与凭据实现，仅使用合成状态；没有把预览读数作为生产测速。

入口：`MenuBarRateImage.swift`、`MenuBarController.swift`、`MenuBarRatePresentation.swift`；预览工具为 `script/preview_menubar.py`。

## 构建与更新验收

- 141 项 Swift 测试、39 项 Python 测试、Go race、动态依赖、严格嵌套签名与适用隔离运行组件检查通过。
- 首轮完整验证在本地 IPv6 认证 HTTP fixture 的一次请求上出现 unexpected EOF，因此没有直接放行。对同一冻结签名包进行三轮该项复测及一次完整隔离集成复测，均通过；完整复测耗时 127.102 秒。没有修改测试或网络实现以使其通过，也未宣称修复该未能再次复现的瞬时失败。原始与复测日志均保留；初始 wrapper 因该失败未完成 export，已签名 ZIP 在其清理前冻结，后续检查均使用这份相同字节。
- 第三方源码对实际最终 App 完整审计：138 条来源 SHA-256、122 个实际链接 Go 模块 h1、15 个来源与 13 份配方/SBOM、Sparkle 源码和许可证、45 项组件元数据检查通过。
- 实际已安装 1.0.3 的隔离副本真实更新至 1.0.4，完成下载、验签、安装和 Sparkle 重启。旧 PID 64751，新 PID 66120，单主进程，全部文件匹配签名目标，严格签名通过。
- 模拟 VPN 在检查/下载阶段保持开启；草稿阻止最终安装且不先断开，清除草稿并明确重试后才正常停止并安装。实际篡改 feed 被拒绝，HTTP 记录没有 ZIP 下载，取消后入口恢复。
- 四附件 GitHub 状态、尺寸和完整 digest 匹配冻结清单。匿名公开 latest 页面跳转至 v1.0.4；完整下载应用 ZIP、feed、SHA256SUMS 并验签。源码附件采用本地全包审计、GitHub 全包 digest 及匿名前 64 KiB 比较，未重复完整下载 216 MB。
- 固定生产 URL 的不附显式代理匿名 curl 与原生 Foundation 请求均返回 HTTP 200、1.0.4/build 61，匹配发布附件并通过官方 Sparkle 验签。某次显式 loopback 代理请求曾短暂返回 CDN 缓存的 1.0.3；未改变固定 URL 或添加查询参数，真实客户端随后识别 1.0.4。
- 已连接的正式 GoConnect 1.0.3 通过生产入口实际显示“1.0.4 可供下载”，停在更新提示，下载及安装留给用户确认。当前 VPN 保持连接。

Transport 相对 v1.0.3 只有生成的版本常量变化。AnyConnect 真实 UDP 仍按用户决定跳过，未修复、未重新验收；不把隔离 UDP／HY2 结果扩大为该能力通过。

## 签名、资产与保护

沿用 RX44JC39GB 的 Apple Development 团队签名及 Hardened Runtime；ZIP 与整个 feed 用原 GoConnect Ed25519 密钥分别签名。公钥、团队、生产 Bundle ID、固定 feed 与 stable 渠道保持兼容。未采用 Developer ID，未做 Apple 公证。

| 附件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| GoConnect-1.0.4-macos-arm64.zip | 31690836 | `b051f612a32d4ca5b92730e711b34b1dca8967130c52be6d9115287859b88b56` |
| GoConnect-1.0.4-third-party-sources.tar.gz | 216709006 | `8946d26064fc2aa2aa16c6c5255c4549544bfc03f019107fb69b4cdca71a505d` |
| appcast.xml | 1504 | `f8cac6d68288d629bc167cb0269590d794b5d3311a5e9a6c4128547ecdffc684` |
| SHA256SUMS | 285 | `75d99a9daa8e5ecaf737ec185eb1a0dcc7f929be638c95f617357259ada8b2ba` |

交付前的保护读回确认：正式 App 文件、四份受保护配置、原 App 与服务 PID/启动时间、原 Git 索引均保持不变。本轮没有替换正式 App、退出它、断开 VPN、升级 helper 或覆盖数据。用户自行安装后，应用及网络服务仍按既有正常更新流程管理。

保留包为 current 1.0.4/build 61、rollback 1.0.3/build 60，每处仅一个 ZIP 和 SHA256SUMS，索引见 `dist/releases.json`。原未提交源码和私有技术历史保留；本轮临时 worktree、构建/SDK/下载、测试 App 与偏好清除，证据单独保留。

## 证据

私有目录：`/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-1.0.4-132900/`。

主要文件：`menu-bar-acceptance.json`、`compact-final.json`、`compact-boundary-final.json`、`compact-off-final.json`、`boundary-window.png`、`source-freeze.json`、`candidate-package.json`、`full-build.log`、`ipv6-auth-repeat.log`、`integration-repeat.log`、`qa-ota-readback.json`、`qa-tamper-readback.json`、`third-party-validation-1.0.4.json`、`public-release-readback.json`、`public-feed-readback.json`、`native-feed-verified.json`、`main-readback.json`、`protection-readback.json`、`production-update-offer.png`、`delivery-final-readback.json` 和 `cleanup.json`。

本轮由 Codex 实现、审核、验收和发布，未调用 Claude、Agy 或子 agent。
