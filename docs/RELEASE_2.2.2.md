# GoConnect 2.2.2（build 87）

## VLESS + REALITY 线路

- 在「线路节点」刷新 HTTPS 订阅，或重新导入本地 Clash YAML，即可看到旧版跳过的 REALITY 节点。VLESS 已验证 TCP（含 Vision）和 gRPC 两种传输。
- 导入时校验 REALITY 公钥与短 ID，保留短 ID 的前导零，以及显式配置的 VLESS 加密参数。带有无效参数的节点会被跳过，同一份文件中的其他有效节点仍可使用。
- 当前内核的 WebSocket 传输不会使用 REALITY 参数，因此导入时会跳过这个组合。文件中所有节点均被跳过时，界面会提示文件里没有受支持的代理节点。

## 安装与兼容性

- 适用于 Apple Silicon Mac，要求 macOS 26.0 或更高版本。
- 应用采用 Apple Development 团队签名（RX44JC39GB）和 Hardened Runtime，未经过 Apple 公证。应用内更新通过 Ed25519 验证更新源和安装包。
- 检查和下载更新时可保持 VPN 连接。保存线路草稿后再确认安装重启。更新后若「诊断」页提示升级网络服务，请按页面提示操作。
- AnyConnect 公司副连接沿用 IPv4 / TCP 使用范围。UDP 仍有已知偶发超时。
