# GoConnect 1.0.4（build 61）

发布日期：2026-11-06。

收紧开启网络速率后的菜单栏宽度。双向速率由横向并排改为上下两行，固定占用从 192pt 减至 90pt，减少约 53%；仅下行或仅上行居中显示一行，也使用 90pt，切换方向时不移动邻近菜单项。关闭速率后恢复系统方形图标，默认仍关闭。

- 保留完整 B/s、KB/s、MB/s、GB/s 单位、连接状态徽章及工具提示/辅助功能语义。
- 保留原生 NSStatusBarButton 的菜单、点击、高亮和系统模板着色。图标与文字合成为可缩放的单色模板，数字使用等宽字体；不新增网络查询。
- 覆盖真实零值、5 秒过期、极值和单位四舍五入边界，固定宽度不随实时样本变化。
- 沿用 1.0.3 的连接中检查、下载及验签流程，确认安装重启后才正常断开 VPN。更新器、VPN 协议、路由规则、配置格式及数据统计口径未改变。

## 安装与验证边界

Apple Silicon；macOS 26 或更新版本。1.0.3 用户可保持 VPN，通过 GoConnect 菜单或设置检查更新，下载验签完成后确认安装。重启后自行重新连接；网络服务如提示新版本，按正常本机授权流程升级。

团队 RX44JC39GB 的 Apple Development 签名及 Hardened Runtime、既有 GoConnect Ed25519 更新密钥和固定签名源保持不变；ZIP 与整个 feed 分别签名。未采用 Developer ID，未做 Apple 公证。

发布须通过完整构建/脚本/Swift/Go race/嵌套签名检查、独立原生菜单栏实际渲染与宽度读回、隔离旧版真实 OTA、四附件公开读回及固定更新源验签。预览速率和 QA 连接状态为模拟数据，不代表生产线路测速；未逐机覆盖全部屏幕缩放、刘海屏和菜单栏拥挤组合。

AnyConnect 真实 UDP 稳定性仍按用户决定跳过，未修复、未重新验收。

## 附件

- `GoConnect-1.0.4-macos-arm64.zip`
- `GoConnect-1.0.4-third-party-sources.tar.gz`
- `appcast.xml`
- `SHA256SUMS`

[发布流程](RELEASING.md) · [固定签名更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)
