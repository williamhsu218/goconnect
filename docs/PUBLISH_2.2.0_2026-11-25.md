# GoConnect 2.2.0（build 85）发布记录

2026 年 11 月 25 日，GoConnect 2.2.0 已发布为 GitHub stable/latest。应用内固定更新源已同步，并通过匿名回验。维护者已确认本机全局模式的 IPv4 验收通过。 [GitHub Release](https://github.com/williamhsu218/goconnect/releases/tag/v2.2.0) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 值 |
| --- | --- |
| 源码提交 | `b3fa950a7bf189687751bc19e5a1fb54828e9a24` |
| 标注标签 v2.2.0 的 Git 对象 | `c01d2671247188e7aa16292d8d2756cbc001685f` |
| GitHub Release ID | `457472585` |
| updates 提交 | `88d971843c6e8c321f4c34d089a7114e174acbb3` |
| 系统要求 | Apple Silicon，macOS 26.0 或更高版本 |
| 应用签名 | Apple Development 团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 沿用既有 Ed25519 公钥，完整 XML 和 ZIP 分别验签 |

## 构建、网络与更新

- 47 项 Python 测试和 239 项 Swift 测试通过。Go race、包内 Mihomo 与 OpenConnect 集成检查通过。主应用和内嵌代码共核对 22 项签名。来源附件含 141 项来源、203 个归档文件，覆盖内核 `v1.19.34-goconnect1` 的对应源码和补丁。
- 在现有全局 VPN 会话中，签名候选的备用节点测速通过物理网卡出站。三次 HTTPS 请求均取得 HTTP 204。五个 CLI 服务入口均通过当前代理完成 TLS 并返回 HTTP 响应。上述请求仅验证网络传输。
- 用项目锁定的 Sparkle 2.10.1 对运行中的隔离版执行 2.1.1（84）到 2.2.0（85）的真实下载、验签和安装。应用以新 PID 单实例重启，175 个文件与符号链接和签名目标完全一致，隔离测试数据保留。签名被篡改的 feed 被拒绝，服务器只收到 XML 请求。
- 自动化测试覆盖 VPN 连接中下载、未保存草稿阻止最终安装、自动检查后释放连接保护。浅色与深色诊断页经过原生渲染检查。正式安装后的更新检查与诊断交互见下方实机验收。

## 公开核对

草稿中的四个附件均已上传，核对了名称、大小、GitHub 提供的 SHA-256 及标签对应的源码。Release 公开后，四个附件均已匿名完整下载，字节数和 SHA-256 与冻结附件一致。生产源提升后，通过客户端固定 URL 重新下载的 XML 与已发布附件逐字节一致。通过当时已连接的生产 VPN 读取同一固定地址，也得到了相同的已签名 XML。使用 2.1.1 更新组件的隔离实例从该地址查到了 2.2.0。

| 附件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.2.0-macos-arm64.zip` | 36457035 | `538e0e4c250fc6109e905ad5cba3be89f7a90fec6a3c6b97e8095ec6927d458f` |
| `GoConnect-2.2.0-third-party-sources.tar.gz` | 227537917 | `663861b6c203f35cfee4aebd02a5756fb706ea6c63e719ebab56d797a47caf73` |
| `appcast.xml` | 1548 | `e4d7ccfc10b2eef78f5218e12cb9c7d878e23086e7c943f6ac23cf5c14ddd55c` |
| `SHA256SUMS` | 285 | `3bae1d6d95155279ad5c4b770511530c04d23553e2a5ab778155a6504c95175a` |

## 正式安装后的实机验收

- 本机手动更新至 2.2.0（85）。已安装应用与公开正式 ZIP 的 175 个文件及链接一致，团队签名通过。正在使用的特权服务和 Mihomo v1.19.34-goconnect1 与包内组件一致。
- 在当前全局线路实测 IPv4。普通请求和显式代理使用同一公网出口，配置中的直连域名从物理网卡出站。UDP 和 TCP DNS 查询均有响应，代理 HTTPS、代理 DoH 和原网络 HTTPS 均可用。浏览器加载了发布页及跨域图片。
- 五个 CLI 服务入口两轮均完成 TLS 握手并返回 HTTP 响应。Antigravity CLI 两次列出 28 个模型标识。当前线路的收发字节和采样写入本机流量统计。
- 检查更新显示已是最新版。随后 50 秒内保持同一 VPN 会话，7 次代理 HTTPS 均成功。同期的主队列探针延迟待单独复现。之后另有一次断开请求，3 秒后重连。该请求的发起入口未记录，当时没有正在运行的验收操作。
- 本机验收范围为全局模式 IPv4。名单模式、主动断开后的 DNS 回收、IPv6 实链路、账号额度和正式环境的安装保护交互未列入本轮。真实 OTA 与更新保护的发布前证据见上文。

维护者在 2026 年 11 月 25 日确认通过。本地归档经 `retain` 和 `check` 核对，正式版为 2.2.0（85），回滚版为 2.1.1（84）。

AnyConnect UDP 的既有偶发超时沿用项目已接受的非阻断范围。本轮原始证据保存在项目外私有目录 `goconnect-release-2.2.0`。 [发布规范](RELEASING.md)
