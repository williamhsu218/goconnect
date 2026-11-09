# 名单代理 DNS 与浏览器本地网络权限

## 原因与修正

1.0.13 的名单代理为未匹配直连/本地区域的系统查询生成 IPv4 和 ULA IPv6 映射。应用发起连接后才按进程选择代理或原网络解析，避免把所有应用的系统查询都送到代理。ULA 映射却会让 Chromium 将公网目标认作本地网络设备，跨来源请求因此触发 Local Network Access 权限。

1.0.14 / build 74 候选只将名单模式的 `dns.fake-ip-range6` 显式置空。保持全局及 DNS 的 `ipv6=true`、IPv6 TUN、IPv4 映射、进程规则、直连和补充解析器策略。固定内核 Mihomo 1.19.30 在不存在 IPv6 映射池时，对延迟解析的 AAAA 查询本地回答成功且无记录，不查询上游；过滤区域仍返回真实 AAAA，出站恢复域名后可以实际连接 IPv6-only 目标。HTTPS 地址提示仍由内核抑制。

这不禁用全部 IPv6，也不改变浏览器权限。依赖系统查询且只接受 AAAA、不接受 IPv4 映射的专用客户端，在名单模式的延迟解析区域仍有兼容限制；真实内网 AAAA、应用自身解析和全局模式不受此项抑制。

## 已完成的隔离检查

- 对原配置运行新增真实内核回归，因公网 AAAA 暴露虚拟 IPv6 而失败；修正后通过。
- 使用实际包内内核分别检查 UDP/TCP：公网 A 保持延迟映射；AAAA/HTTPS 无上游查询或虚拟 IPv6；本地、单标签、直连和补充区域仍返回真实 IPv6。
- 全局 AI AAAA 查询经代理 DoH 返回真实记录，不调用原网络 DNS；原有代理故障不回落验证保留。
- 名单中连接将域名交给代理；名单外连接使用原 DNS，并实际读取仅监听 IPv6 的 HTTP 服务；代理失效不会回落原网络。
- Chrome 156.0.8078.80 原生界面对照：回环验收页使用 `Content-Security-Policy: treat-as-public-address` 标明公共地址空间，仅以 `credentials: omit` 请求同一公网目标的 `robots.txt`。旧 ULA 地址实际弹出本地网络权限，关闭提示而未授权；IPv4 映射请求实际完成，未出现该提示。该对照沿用正在运行的旧版 VPN，未切换系统 DNS，也未修改站点权限。

## 待完成的安装验收

隔离检查不等于候选已在生产连接生效。当前安装版和活跃 VPN 保留 1.0.13；须在用户允许的窗口正常升级候选 App/网络服务并重连，检查系统 A/AAAA、名单代理与全局策略、实际公网网页、AI 代理和公司内网。副 VPN 在公司局域网中保持关闭，不要求公司密码。

安装前后保留原包及数据、核对连接退出清理；更新结果对话框保持至少 45 秒时确认所有生产组件、租约续期和真实请求持续正常。没有完成实际安装及网页验收，不将本候选写为正式验收通过，不提升生产更新源。

原始记录保留在项目外私有目录 `~/.codex/visualizations/2026/11/09/goconnect-browser-dns-fix/`。候选构建、签名和最终检查结果以该目录及后续安装记录为准。

## 依据

- [Chromium IP 地址空间分类源码](https://github.com/chromium/chromium/blob/156.0.8078.80/services/network/public/cpp/ip_address_space_util.cc)：ULA 属于 local；本次实测浏览器的 IPv4 benchmarking 映射不在 local 表内。不能将本次结果无条件推广到未来浏览器版本。
- [Mihomo 1.19.30 DNS middleware](https://github.com/MetaCubeX/mihomo/blob/v1.19.30/dns/middleware.go)：fake-IP 过滤优先，没有 IPv6 映射池时 AAAA 返回空答复；真实解析路径保留 IPv6。
- [Chrome Local Network Access](https://developer.chrome.com/blog/local-network-access)：保留浏览器对真实本地网络访问的保护。
