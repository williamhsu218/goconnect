# GoConnect 1.0.5（build 63）

发布日期：2026-11-06。

修复 IPv4 物理网络上连接时报“无法添加透明路由 ::/1：新增路由未通过精确查表验证”，并收紧固定宽度菜单栏速率的外侧留白。

## 连接修复

实际故障现场的 TUN 只有 IPv4 地址，IPv6 地址未配置，因而无法建立 ::/1。Mihomo 1.19.30 在系统没有全局单播 IPv6 地址时会省略 TUN 的 IPv6 配置；GoConnect 的手动双栈路由计划依赖该内部地址。

本版仅为 GoConnect 管理的 TUN 核心子进程设置官方提供的 SKIP_SYSTEM_IPV6_CHECK=1，保留内部双栈地址。不更改系统 IPv6 设置，不影响纯订阅代理进程；继续保留精确路由所有权、网关、退出清理和 helper CDHash 验证。没有跳过 ::/1 或忽略查表错误。

参考：[Mihomo 官方 TUN 说明](https://wiki.metacubex.one/config/inbound/tun/#inet6-address)；[对应 1.19.30 版本检测逻辑](https://github.com/MetaCubeX/mihomo/blob/v1.19.30/config/utils.go)。

## 菜单栏

- 按最长格式化速率一次确定固定最大宽度；图标与文字间距缩至 2pt，外侧总边距仅 2pt，文字靠右排列，减少短速率右侧的空白。
- 双向仍为上下两行，单向一行；完整字节/秒单位、状态徽章、零值、过期数据和辅助功能保留。零值、断开、过期样本、单位或显示方向切换均不改变开启速率时的占用宽度。
- 默认关闭速率；关闭后恢复系统方形按钮。没有按实时样本自适应宽度，邻近菜单项不会随数字跳动。

## 安装与验证范围

Apple Silicon，macOS 26 或更新版本。通过 GoConnect 检查、下载和验签时可保持 VPN；确认安装后正常断开并重启。安装后按应用提示更新本机网络服务，本版连接修复需要新的 Runtime 正常生效。

沿用 RX44JC39GB 的 Apple Development 团队签名、Hardened Runtime 和原 GoConnect Ed25519 更新密钥。ZIP 与完整 feed 分别签名，固定地址和公钥不变。未采用 Developer ID，未做 Apple 公证。

发布须完成全套检查、实际原生 TUN 双栈路由与连接/清理验收、菜单栏实际内容及像素边界读回、隔离真实 OTA、公开附件及固定签名更新源回验。模拟数据不代表生产测速；AnyConnect 真实 UDP 稳定性仍按用户决定跳过，未修复、未重新验收。

附件：应用 ZIP、对应第三方源码包、已签名 appcast.xml、SHA256SUMS。
