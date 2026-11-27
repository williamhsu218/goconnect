# GoConnect 2.2.3（build 88）

## VLESS TCP + REALITY

- 本地 Clash YAML 与 HTTPS 订阅中的 TCP + REALITY 节点可用 `tcp-receive-buffer` 设置外层 TCP 的接收缓冲，数值按字节填写。例如 `tcp-receive-buffer: 8388608` 为 8 MiB。
- 省略该项或设为 `0` 时，macOS 继续按连接自动调整接收缓冲。显式设置会在 TCP 建连前应用。若本机内核无法采用指定值，该节点连接会失败，可移除此项后重试。

## 安装与兼容性

- 适用于 Apple Silicon Mac，要求 macOS 26.0 或更高版本。
- 应用采用 Apple Development 团队签名（RX44JC39GB）和 Hardened Runtime，尚未经过 Apple 公证。应用内更新验证更新源和安装包的 Ed25519 签名。
- 检查和下载更新时可保持 VPN 连接。保存线路草稿后再确认安装重启。更新后若「诊断」页提示升级网络服务，请按页面提示操作。
- AnyConnect 公司副连接沿用 IPv4 / TCP 使用范围，UDP 仍有已知偶发超时。
