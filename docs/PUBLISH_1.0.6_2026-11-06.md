# GoConnect 1.0.6 设置快捷入口发布

2026-11-06；stable 1.0.6/build 65。Release ID `436185414`，latest，非 draft、非 prerelease。[公开发布页](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.6)。

## 变更及界面验收

菜单栏原生快捷菜单在“诊断”下方、“退出”分隔线之前增加“设置…”（⌘,）。通过应用生命周期持有的 SwiftUI `openSettings` 环境动作打开和前置原生 Settings scene；保留启动期间的待处理动作。不模拟按键、不使用私有选择器，不改 VPN 或配置逻辑。

隔离预览使用真实 MenuBarController 构造 NSMenu；CUA 点击 QA 按钮，由其派发同一个 NSMenuItem 和生产回调。确认菜单顺序、逗号快捷键和启用状态正确。重复调用复用原生设置窗口；关闭主窗口后再派发菜单项，实际打开设置。读回 `settingsRequestHadVisibleMain=false`、`visibleSettingsWindows=1`，组件哈希与发布源码一致。这里是实际菜单项动作验收，不表述为鼠标直接点选系统菜单栏的完整录屏。

保留 1.0.5 的 77pt 固定紧凑布局与完整 MB/s 等单位，以及内部双栈 TUN 修复。本版 Transport 相对 v1.0.5 仅生成版本常量变化，不触碰路由实现。真实双栈路由、连接、清理和原配置保护证据见 [1.0.5 发布记录](PUBLISH_1.0.5_2026-11-06.md)。

## 源码、构建和签名

- 发布源码/tag：`f98bc928ee8e112779d1052e2afefbacaa2d877a` / `v1.0.6`；树 `d260524c099595af3f3d9bafcf81416e14520833`。
- 构建代码提交 `d3a1f5f4ff4c523dced92f469ad41b8d4a247339`；其后仅扩展独立 `script/preview_menubar.swift` 的验收记录和动作，所有 App、核心、版本、包配置及构建输入保持一致。完整文件哈希和差异见 `source-freeze.json`。
- main 合并 `5cb2d8709c9182ca02e5422cd8e92329c0bf9fb8`，保留前 main 和本版历史，树匹配 tag。签名 updates 提交 `5425f63dde24d8e5184291af98ed2317e57376ee`，从 `174fc92ad3073e6814df3a464f6258fa31a5cbf2` 正常 fast-forward。
- 142 项 Swift、39 项 Python、Go race、动态依赖、严格嵌套签名和完整隔离网络检查通过；完整集成耗时 127.129 秒。
- Apple Development 团队 RX44JC39GB、Hardened Runtime、原 GoConnect Ed25519 更新密钥；ZIP 与完整 XML 分别签名，固定公钥和 feed 不变。未做 Developer ID 分发签名或 Apple 公证。

## 更新链路和公开回验

实际旧版 1.0.5/64 的隔离副本完成下载、验签，并在明确正常退出后由 Sparkle 安装。随后通过 CUA 手动冷启动 1.0.6/65，新的单一主进程和 163 个文件均匹配签名目标，连接模拟入口正常，未残留安装锁。没有把该手动冷启动写成自动重启。篡改 feed 被拒绝、没有 ZIP 请求，取消后恢复入口。

第三方源码完整审计通过：138 个来源 SHA、122 个 Go 模块 h1、15 个组件来源和 13 份配方、Sparkle 源码/许可证、45 项元数据。四个附件的 GitHub 完整 digest、尺寸和上传状态匹配；匿名完整下载 App、feed、SUMS 并验签。源码采用本地全包审计、GitHub 全包 digest 和匿名前 64 KiB 比对，未重复全包下载。

固定生产 URL 的匿名不附显式代理请求先读到新版，但原生 Foundation/Sparkle 的压缩响应短暂命中 GitHub 的旧缓存。保留原 URL、缓存策略及签名字节，等待后原生请求读到完全匹配的 1.0.6/65，并验签通过。7 次定时探测的原始结果保留；不能用最初的公开 curl 成功代替实际客户端识别。

最终，仍连接 Tokyo Japan 的正式 GoConnect 1.0.5 已实际弹出“1.0.6 可供下载”。停在安装确认窗口供用户操作，当前 VPN 保持连接；本轮没有为了设置入口再次退出正式 App 或升级其 helper。1.0.6 尚未替换本机正式安装。

AnyConnect 真实 UDP 仍按用户决定跳过，未修复、未重新验收。

## 资产、保护和证据

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| GoConnect-1.0.6-macos-arm64.zip | 31693825 | `bc38df315a6fd4bbbcd3370731c88b88427809900473df2e21577055bc2e0d33` |
| GoConnect-1.0.6-third-party-sources.tar.gz | 216709121 | `e90dedcf8bd3acf1c4dc181ff5af59fe86b1e6e66b5f8928c6c1dea0331dce2c` |
| appcast.xml | 1523 | `4ac23ee4b8b350570472f2bd91a6aca93aa27635ca69c753557e73cf81b8c398` |
| SHA256SUMS | 285 | `572721a5f81338696456add197304866cf72f78a0f6c8af09b2c75a6019916ac` |

保留包 current 1.0.6/65、rollback 1.0.5/64，每处只有一个 ZIP 和 SHA256SUMS。正式 1.0.5 安装字节、四份受保护配置、原 Git 索引保持一致；真实流量统计持续累计。根工作区原有未提交改动和私有技术记录保留。清除本轮工作树、候选/下载/SDK/测试 App、隔离服务及偏好，保留独立数据备份和私有证据。

证据：`/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-1.0.6-settings/`。主要文件为 `settings-acceptance.json`、`settings-native-readback.json`、`settings-closed.ax.txt`、`settings-open.png`、`source-freeze.json`、`full-build.log`、`qa-ota-readback.json`、`qa-tamper-readback.json`、`third-party-validation-1.0.6.json`、`public-release-readback.json`、`public-feed-readback.json`、`native-feed-verified.json`、`production-update-offer.png`、`main-readback.json`、`delivery-final-readback.json`、`cleanup.json`。

本轮由 Codex 实现、审核、验收与发布，未调用 Claude、Agy 或子 agent。
