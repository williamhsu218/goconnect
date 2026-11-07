# GoConnect 1.0.11 发布与验收记录（2026-11-07）

**发布完成：GitHub 正式 Release / latest、四个附件及签名生产更新源均已公开回验。本机已安装同一份 1.0.11，网络服务已正常升级，主、副 VPN 恢复运行。**

源码提交 `6bada6a76db1a1c272e12d454f8a8ab6419a683a`，版本 1.0.11 / build 71。修复公司 TCP 半关闭通知、已结束连接名额回收，以及取消后无响应上游的两层残留；仅公司通道启用 75 秒有进度刷新空闲排空，主线路和 UDP 保留原策略。公司副连接去掉试用标记，仍默认隐藏，配置与启用状态保留；本机真实验收及公开放行结果如下。

## 已完成的候选检查

- 团队 RX44JC39GB 的 Apple Development + Hardened Runtime；22 个包内 Mach-O 核对通过。不是 Developer ID 分发签名，未经过 Apple 公证。
- 正式 Bundle ID、stable、固定生产 feed、原 Ed25519 公钥与签名账户一致；强制完整 feed 与解包前 ZIP 验签，自动下载/安装关闭，自动检查默认关闭。
- `--verify --team-sign` 完整退出 0，237.65 秒；39 项 Python、193 项 Swift、适用 Go race、打包组件与隔离网络集成通过，集成 127.827 秒。专用已安装 TUN 测试仍按显式门禁跳过，不计为真实线路通过。
- 300 顺序短请求、400,000 字节延迟响应、服务器 EOF、长连接、RST、取消、公司世代撤销与活跃慢响应计时回归通过；重点取消及计时场景 race 重复 10 次通过。详见 [连接回收技术记录](COMPANY_FLOW_RELEASE_2026-11-07.md)。
- 对应最终 App 的第三方源码完整审计通过：138 条源记录（16 组件、122 Go 模块），122 个模块 ZIP 重算 h1、198 个归档文件逐项比对；缺失的 GnuTLS 主源码经同一官方 URL 有界重试及精确 SHA 验证后补齐，未降低校验要求。
- 独立 `com.willhsu.GoConnect.UpdateQA` 实际从 70 下载、验签、安装并自动重启到 71。旧 PID 5936 → 新 PID 6527，连续 12 个 0.2 秒样本单实例；163 个普通文件和 9 个符号链接与目标 QA 包一致，签名通过。QA 复制重签未改变正式 ZIP。
- 模拟 VPN 与草稿开启时真实完成下载；草稿阻止安装，旧 70 保留，解除草稿并明确确认后才结束模拟连接并安装。稍后更新、无更新、自动检查开启后的冷启动及更新失败后均可切换模拟连接。篡改 QA feed 被实际客户端拒绝，ZIP GET 数保持 1。
- 隔离更新期间正式 App、主副 VPN PID 和三份配置哈希保持不变；随后用户另行明确授权维护窗口，才正常断开、安装候选并升级服务。实际安装的 172 个普通文件及符号链接与冻结 App 一致，签名通过；服务二进制 SHA-256 与候选一致。应用和用户数据分别保留保护边界，没有回滚配置。

## 冻结附件

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.11-macos-arm64.zip` | 32127412 | `067eb12966721b322020ad5e09ab0b98595fb5fdb50ea181fb0428d355da3799` |
| `GoConnect-1.0.11-third-party-sources.tar.gz` | 216708925 | `d737c8d633f863a0ab0c691f7f41eff3d096bf0d56b83d7c99d7936359adce49` |
| `appcast.xml` | 1647 | `a19474f19efc48398c126b267d29fc1c91822b67599d7b3e5768036e36059c54` |
| `SHA256SUMS` | 287 | `d89bbf9dc1bd11ad70ae500a8b9203c8d9ef55fc6884a5b1e7bfb65f4ff8e63c` |

四附件先上传 draft 并核对恰好四项、uploaded 状态、尺寸及 GitHub 完整 digest，再公开为 stable/latest。tag 解析后的源码提交与构建记录一致，公开后没有覆盖 tag 或附件。

| 发布项 | 回验结果 |
| --- | --- |
| Release | https://github.com/williamhsu218/goconnect/releases/tag/v1.0.11 |
| Release ID | 437577500 |
| annotated tag 对象 | `3794b1c05728002393279ebb40426d5ab250db1f` |
| updates 提交 | `087dfe74be034815c53e3a000771dadbd7295ef7` |
| 固定更新源 | https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml |

匿名完整下载应用 ZIP、XML 和 SHA256SUMS，大小、逐字节/SHA-256 与冻结附件一致，官方工具验证完整 feed 与 ZIP 的 Ed25519 签名及 enclosure 元数据。第三方源码公开回验采用 GitHub 全包 digest 加匿名 128 KiB Range 比对，**未重新完整下载源码包**；本地完整源码审计已通过。

公开资产回验通过后才将同一份签名 XML fast-forward 推送 updates。固定原 URL 的匿名 curl 与 macOS 原生 URLSession 均首次读回 build 71，XML 1647 字节、SHA-256 与上表相同，签名通过。没有使用临时查询参数绕过固定地址验收，没有 force push 或降低生产 build。

## 真实线路验收

- 主线和公司副线正常建立，公司服务器下发 DNS 后，公司域名通过系统 scoped resolver 及该本地 resolver 的 TCP 查询返回预期私网 IPv4；AAAA 按现有 IPv4 功能边界返回 NOERROR/NODATA，不记为公司 IPv6 业务通过。
- 320 个低速顺序只读请求全部成功，P95 0.12857 秒、最大 0.1733 秒；公司桥接 UNIX socket 在每 40 次采样和最终静置后均为 3，没有累计到 256 上限。独立已登录浏览器连续 6 轮工作台/历史账本切换，均加载完整记录且无超时；没有生成、购买、运行回测或编辑账本。
- 公司 HTTPS 网页、两个现有 Docker 服务、国内网站、公网 204 与局域网路由器正常。TUN 原生 IPv4/IPv6 地址及 IPv4/IPv6 路由读回正确；不将其表述为未单独测试的上游 IPv6 服务能力。
- 仅正常断开公司连接后，公司进程退出、公司 HTTP 不能取得响应、公司 DNS 返回 SERVFAIL，保留公司 scoped resolver 防止回退；主线所有 PID 不变且公网仍返回 204。
- 完整正常断开后，本机服务 ready/idle、全部会话进程退出、TUN、公司路由与公司 DNS 记录均移除。首次观测被用户手动重连覆盖，不作为产品失败；停止干扰后重新检查通过。
- 恢复期间额外的原生 AnyConnect 接管默认路由，GoConnect 按安全预检拒绝叠加，没有强删外部路由或强行连接。用户随后自行断开额外客户端并恢复 GoConnect 主副线路；Tailscale 保持原状态。
- 恢复后再验 64 次请求全部成功，最大 0.13073 秒，socket 前后均为 3；公司 DNS、内网、国内、公网和局域网正常，浏览器再次加载完整历史账本。三份配置哈希与维护前一致，当前双线路保留运行。

## 保留与收尾

保留包已按规范更新为 current 1.0.11 / 71、rollback 1.0.10 / 70，各目录仅一个 ZIP 和 SHA256SUMS，清单检查通过。应用包回滚与用户数据回滚独立。QA App、服务器和安装 helper 已退出，测试偏好精确恢复；过时候选与审查副本已清除。签名 SDK、公开下载、安装前 App/配置备份及本轮 staging 在正式/回滚包核对后清理，不移除正在运行的 App 或网络服务组件。

## 协作和限制

Claude Code 请求固定 claude-opus-5-5 / high，平台模型回执一致，未提供 effort 回执；Agy 请求固定 gemini-4-argon-medium / medium，返回审查文本但无独立模型/强度回执。Codex 实现与集成、纠偏、构建和实际 OTA；取消/计时补验由 Codex 子 agent 完成。模型审查不代替运行证据。

公司业务范围为明确 IPv4 私网及 TCP；不包括 AnyConnect UDP 或 Finder 内核 SMB，睡眠恢复与其他企业环境需针对性验收。历史 AnyConnect UDP 限制未修复、未重新验收。完整包最低 macOS 26.0。

原始证据保留在项目外私有目录 `~/.codex/visualizations/2026/11/07/goconnect-release-1.0.11/`；不把公司地址、凭据或原始业务数据放入公开附件。
