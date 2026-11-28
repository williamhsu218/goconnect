# GoConnect 2.2.4（build 89）

## VLESS 主线路与公司副连接

- 修复开启公司副连接时，VLESS 主线路被“只支持 HY2”提示拦截的问题。现在可选用 VLESS（含 REALITY）或 HY2 作为主线路，再连接独立的 AnyConnect 公司线路。
- 在连接主线路前，打开“设置 → 高级 → 公司副连接”，选择公司线路并填写公司 IPv4 网段和域名。副连接断开后，这些目标会继续被阻断，普通网页仍可使用主线路。
- 已通过本地隔离环境验证 VLESS + REALITY 主线路和公司流量分流。真实公司账号认证、私网访问和系统 TUN 的组合由维护者在手动更新后验收。公司业务范围为 IPv4 / TCP。

## 安装与更新

适用于 Apple Silicon Mac，要求 macOS 26.0 或更高版本。应用采用 Apple Development 团队签名（RX44JC39GB）和 Hardened Runtime，尚未经过 Apple 公证。Sparkle 分别验证更新源和安装包的 Ed25519 签名。

可以保持 VPN 连接检查和下载更新。请先保存线路草稿，再确认安装重启。更新后如“诊断”页提示升级网络服务，可按页面提示操作。
