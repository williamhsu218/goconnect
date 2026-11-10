# 技术记录索引

- [1.0.14 发布说明（浏览器本地网络权限兼容）](RELEASE_1.0.14.md)；[签名、OTA 与公开发布记录](PUBLISH_1.0.14_2026-11-10.md)；[DNS 与浏览器原生验收](DNS_BROWSER_COMPATIBILITY_2026-11-09.md)。
- [1.0.13 发布说明（更新弹窗期间的连接心跳）](RELEASE_1.0.13.md)；[签名、原生与 OTA 发布记录](PUBLISH_1.0.13_2026-11-09.md)。
- [1.0.12 发布说明（副线重复重连与白名单 DNS）](RELEASE_1.0.12.md)；[发布与验收记录](PUBLISH_1.0.12_2026-11-09.md)；[socket 清理](COMPANY_RECONNECT_SOCKET_2026-11-09.md)；[DNS 模式边界](WHITELIST_DNS_BOUNDARY_2026-11-09.md)。

- [1.0.11 发布说明（公司副连接正式功能与连接回收修复）](RELEASE_1.0.11.md) — 默认隐藏，按需开启；[连接回收回归记录](COMPANY_FLOW_RELEASE_2026-11-07.md)；[正式发布与实机/OTA 记录](PUBLISH_1.0.11_2026-11-07.md)。
- [1.0.10 发布说明（公司副连接按需开启）](RELEASE_1.0.10.md) — 默认隐藏入口，设置中手动配置启用，关闭保留配置；[界面与配置保护验收](COMPANY_FEATURE_VISIBILITY_2026-11-07.md)；[正式发布与 OTA 记录](PUBLISH_1.0.10_2026-11-07.md)。

- [无需更新正式 App 的临时公司通道测试器](COMPANY_TUNNEL_TESTER.md) — 用户态副隧道、物理出口校验与只读 DNS/内网探测；[两轮实链路验收](COMPANY_TUNNEL_LIVE_2026-11-07.md)。

- [双 VPN 测试构建与验收边界](DUAL_CONNECTION_TEST_2026-11-07.md)；[方案](DUAL_CONNECTION_PLAN_2026-11-07.md)；[分块审查](DUAL_CONNECTION_REVIEW_2026-11-07.md)。

- [1.0.9 发布说明（公司副连接试用）](RELEASE_1.0.9.md) — 默认关闭，真实场景验收待用户更新后完成；[发布与 OTA 记录](PUBLISH_1.0.9_2026-11-07.md)。
- [1.0.8 发布说明](RELEASE_1.0.8.md) — 系统 DNS 接管、国内直连和私有域名保护；[实现与验收边界](SYSTEM_DNS_2026-11-07.md)；[发布验收记录](PUBLISH_1.0.8_2026-11-07.md)。
- [1.0.7 发布说明](RELEASE_1.0.7.md) — HY2 公网 DNS 可靠解析；[实现与验收边界](RELIABLE_DNS_2026-11-06.md)。

- [1.0.6 发布说明](RELEASE_1.0.6.md) — 菜单栏原生设置快捷入口。
- [1.0.5：IPv4 网络下 TUN IPv6 初始化修复与菜单栏内容宽度](RELEASE_1.0.5.md)

- [1.0.4：菜单栏速率收紧为 90pt](RELEASE_1.0.4.md)

- [1.0.3：连接中下载更新与更新后连接锁修复](RELEASE_1.0.3.md)

- [1.0.2：菜单栏图标与可选上下行速率](RELEASE_1.0.2.md)；[正式发布流程](RELEASING.md)
- [菜单栏实现与隔离验收](MENUBAR_RATE_2026-11-06.md)

- [1.0.1：团队开发签名、GitHub 在线更新与图标](RELEASE_1.0.1.md)；[更新与签名维护](UPDATES.md)

- [0.11.17 预发布：浅色精密界面、按线路流量统计与代理名单命名](RELEASE_0.11.17.md)

- [历史正式版 0.11.14：修复汇总、验收边界与收尾](RELEASE_0.11.14.md)；此前历史回滚版为 [0.11.13](RELEASE_0.11.13.md)
- [Argon 后续建议核查：UDP 目标隔离、恢复触发与 0.11.14 源码候选](ARGON_FOLLOWUP_0.11.14_2026-10-18.md)
- [0.11.14 安装验收：30 分钟纯负载、恢复补测与一次强制重连 UDP 超时](CANDIDATE_INSTALL_0.11.14.md)
- [构建、版本源、临时产物和发布包保留边界](BUILD.md)
- [Argon 分析逐项核查与本轮修复](ARGON_REVIEW_2026-10-17.md)
- [连接、健康检查与退出清理复查](CONNECTION_LIFECYCLE_AUDIT_2026-10-17.md)
- [视频渲染高负载断线诊断](INCIDENT_2026-10-17_RENDER_LOAD.md) 与 [超时修复及隔离验收](LOAD_TIMEOUT_FIX_2026-10-18.md)
- [分层容错、自动恢复与高负载隔离验收](LAYERED_RECOVERY_2026-10-18.md)
- [0.11.11 候选安装与服务更新的历史记录](CANDIDATE_INSTALL_0.11.11.md)
- [0.11.11 真实 VPN、10 分钟高负载与恢复验收](RELEASE_0.11.11.md)
- [HTTPS / UDP 超时复查、后台调度问题与 0.11.12 候选](REQUEST_TIMEOUT_INVESTIGATION_2026-10-18.md)
- [0.11.12 安装、14 分钟真实 VPN 渲染与恢复验收；UDP 未通过](CANDIDATE_INSTALL_0.11.12.md)
- [UDP 桥接诊断、失效连接修复与 0.11.13 候选](BRIDGE_UDP_DIAGNOSTICS_2026-10-18.md)
- [0.11.13 安装与真实 VPN 验收](CANDIDATE_INSTALL_0.11.13.md)
- [0.11.13 真实 VPN、30 分钟渲染与 HTTPS / UDP 持续验收](LONG_LOAD_30MIN_0.11.13_2026-10-18.md)
- [脱敏诊断事件与退出原因](DIAGNOSTICS.md)
- [公开源码审计边界](AUDIT.md)
- [0.11.11 历史验收](RELEASE_0.11.11.md) 与 [0.11.10 历史发布](RELEASE_0.11.10.md)

历史发布、故障和验收记录保留原路径及其当时的证据范围；不能据此推断当前运行状态。源码候选版本见根目录 VERSION.json，保留包版本见本机 dist/releases.json。

- [双 VPN 功能测试版与单独窗口验收](DUAL_CONNECTION_INTEGRATED_TEST_2026-11-07.md)
