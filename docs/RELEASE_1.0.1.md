# GoConnect 1.0.1（build 58）

发布日期：2026-11-06。

本版加入团队开发签名、基于 GitHub 的签名在线更新，以及表达应用分流的双路径 G 图标，保留浅色精密界面和本机按线路流量统计。

## 更新内容

- 主程序、网络运行组件、Sparkle 框架及内嵌程序采用同一团队身份逐层签名，并启用 Hardened Runtime。
- 应用菜单和设置增加“检查更新”；自动检查默认关闭，可主动开启，自动下载和安装保持关闭。
- 固定 GitHub HTTPS feed，更新 ZIP 和整个 feed 分别使用 Ed25519 签名，解包前验证归档。
- VPN、服务操作、节点测试、订阅操作及未保存草稿存在时保护更新流程；准备安装期间阻止新连接，退出前再次检查。
- 处理更新中断后残留状态：保留退出保护，同时避免旧标记永久锁住联网。
- 新图标以蓝青双色路径、G 轮廓及端点表达线路选择，适配 macOS 图标留白与小尺寸显示。
- 不改变已有分流配置、线路、订阅格式、密码保存设置或本机流量数据库格式。

## 签名与更新

沿用现有团队的 **Apple Development 开发签名**方式，并独立使用 GoConnect 的 Ed25519 钥匙串密钥。此包未采用 Developer ID 分发签名，未经过 Apple 公证；本机更新验收不能代表所有其他 Mac 的首次安装信任行为。本项目不提供关闭系统安全保护的方法。

0.11.17 没有内置更新器，首次迁移需要先正常断开 VPN、退出，再替换应用。更新 App 后，按诊断页需要正常更新本机网络服务；应用包、helper 和用户数据分别管理。

正式更新 feed 位于固定 GitHub 分支，文件也附在本次 Release：
[签名更新 feed](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

维护及保护边界见 [更新说明](https://github.com/williamhsu218/goconnect/blob/v1.0.1/docs/UPDATES.md)。

## 验收与已知限制

已通过 Swift 全套 126 项、更新相关回归、39 项脚本测试、Go race、严格嵌套签名、运行组件和隔离网络集成检查。安装于本机后已恢复原 HY2 订阅线路，原配置、订阅和旧凭据保留，原本机流量累计继续增长。

独立签名 UpdateQA App 真实完成 1.0.0 → 1.0.1 的下载、验签、安装和重启，安装文件与签名目标一致，仅保留一个主进程。篡改 feed 被客户端拒绝，没有下载更新包；取消后恢复可操作状态。生产 URL、归档及 feed 签名另做公开读回核验。

**AnyConnect 的真实 UDP 稳定性仍有既往随机超时记录，未修复、未重新验收。** 本次按明确接受的 HY2、界面、数据保护、签名和在线更新范围正式发布，该限制继续保留，不将其他线路成功当成 AnyConnect UDP 通过。

## 附件

- `GoConnect-1.0.1-macos-arm64.zip`：Apple Silicon 应用，要求 macOS 26 或更新版本。
- `GoConnect-1.0.1-third-party-sources.tar.gz`：15 份既有上游源码／补丁、122 个实际链接 Go 模块及新增 Sparkle 2.10.1 源码、许可证与来源记录，共 138 条来源记录。
- `appcast.xml`：已签名的正式更新 feed。
- `SHA256SUMS`：应用包、第三方源码及 feed 的完整 SHA-256 校验值。

下载后可运行 `shasum -a 256 -c SHA256SUMS` 校验附件。
