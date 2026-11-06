# GoConnect 1.0.7（build 67）

发布日期：2026-11-06。

修复 HY2 线路中公网 UDP DNS 查询可能因 UDP 回程丢包而超时的问题：由节点内核接收查询，再通过所选 HY2 线路的可靠 HTTPS 通道访问 Cloudflare／Google DoH。保留证书校验，禁用 DoH 的 HTTP/3；不修改 macOS 系统 DNS。

仅作用于经过 HY2 代理的公网 UDP 53 请求。内网、回环、链路本地及 Tailscale/CGNAT DNS 目的地址保留原有路径，其他 UDP 和非 HY2 协议不变。此修复不代表普通 UDP 丢包或 AnyConnect UDP 稳定性已解决。菜单栏固定紧凑宽度、完整速率单位和“设置…”快捷入口继续保留。

## 更新与系统要求

Apple Silicon，macOS 26 或更新版本。在 GoConnect 内检查更新，可保持 VPN 检查、下载和验签；确认安装时正常断开。新版涉及网络组件，更新后如诊断页提示，请通过“更新服务”完成本机管理员授权，再连接。

正式 build 67 高于本地验证候选 build 66，候选用户也可通过应用内更新获得此版本。

沿用 RX44JC39GB 的 Apple Development 团队签名、Hardened Runtime 和既有 GoConnect Ed25519 更新密钥；应用 ZIP 与完整 feed 分别签名。固定生产更新地址、公钥及 stable 渠道保持不变。未采用 Developer ID，未做 Apple 公证。

## 验证与边界

发布门禁包括完整构建及回归测试、真实内核 DNS/DoH 路由测试、HY2 双端口实链路查询、安装后运行证据、隔离真实 OTA、签名和四附件公开回验。不同域名的无重试查询用于避免缓存掩盖传输失败；内网 DNS 与非 DNS UDP 的隔离测试不代表所有企业网络或应用都已验收。

AnyConnect 真实 UDP 历史问题未修复、未重新验收。此版本不提供 VPN 服务器、节点或凭据。

附件：应用 ZIP、对应第三方源码包、签名 appcast.xml、SHA256SUMS。
