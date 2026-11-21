# GoConnect 2.0.1（build 81）

## 原网络 HTTPS 诊断

- 修复部分网络能正常访问 HTTPS、旧探测地址却连接超时的问题。诊断现在优先在原网卡上连接阿里云的固定地址并验证 HTTPS 证书，Cloudflare 和 Google 地址用作备用。
- 已在本机验证原网 IPv4 和 IPv6 探测，以及同一 VPN 会话中的代理 HTTPS 和代理 DoH 检测。

## 系统要求与更新

- 适用于 Apple Silicon Mac，macOS 26.0 或更高版本。签名采用 Apple Development 团队证书和 Hardened Runtime，应用未经过 Apple 公证。
- 在 GoConnect 菜单中选择「检查更新」。下载和验签期间可保持 VPN 连接，确认安装重启时应用会正常断开连接。更新后如提示服务版本不符，请在「诊断」升级本机网络服务。
- 签名在线更新在隔离环境中验证。生产环境实测在安装后进行。AnyConnect UDP 的历史验收范围见发布规范。 [发布规范](RELEASING.md)
