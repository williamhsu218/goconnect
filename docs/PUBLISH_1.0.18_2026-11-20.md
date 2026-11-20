# GoConnect 1.0.18 (build 78) 发布验收

2026-11-20 发布为 GitHub stable/latest，签名生产更新源已公开验签。正式 GoConnect 1.0.17 已恢复 Tokyo Japan 名单代理，并在 App 内实际识别到 1.0.18。 [GitHub Releases](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.18) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 结果 |
| --- | --- |
| 构建源码 | `d23b5ade517488bb4fb48bdfd0a11ccd3bede893` |
| 标注 tag | `v1.0.18`，tag 对象 `942c34c50ecdc153007e09243191b3360107d774` |
| GitHub Release ID | `452350123` |
| updates 提交 | `fc3e18acbcce63e8415e7d323c870f6c4521de20` |
| 系统要求 | Apple Silicon，macOS 26.0 或更高 |
| 代码签名 | Apple Development，团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 既有 Ed25519 公钥，stable，完整 XML 和 ZIP 分别验签 |

## 构建与实网

- 226 项 Swift 测试、4 个 Go 包的 race 测试、隔离运行与打包内核检查通过。[12 项网络审查复核](AUDIT_1.0.17_2026-11-20.md)记录源码改动和回归。真实多网卡切换、包外签名子进程与公司副线的另行实网测试未在本轮重跑。
- 独立签名测试版在物理网络无非链路本地 IPv6 地址时，按名单和全局模式均获得预期 IPv4／IPv6 TUN 地址及精确路由。两种模式的代理 HTTPS、DoH、直连 HTTPS 健康探测均成功。Anthropic 与 OpenAI API 分别返回未认证 HTTP 401，百度 HTTPS 返回 200。
- 名单模式下原生解析返回保护域名的 IPv4 映射和空 AAAA，直连域名由实际安装的解析器返回真实 A。百度子域名的 macOS CNAME 目标曾复用旧映射。直接读取接管的解析器得到真实 CNAME 与 A，随后 HTTPS 正常。全局模式下系统 DNS 返回真实公网地址。
- 两种模式中 Chrome 158.0.8127.36 均打开云悠真实登录页，并观察到跨域字体样式和 Cloudflare 脚本资源。名单模式下原生浏览器界面未弹出本地网络权限请求，两次页面观察均未记录相关控制台错误。
- 两次正常断开后均确认各自的 TUN 分流路由清除。最终测试专用服务已卸载。1.0.17 回连曾再遇到同一条系统路由缓存，缓存自行过期后恢复。正式 App 及三个配置文件的摘要匹配验收前，主线路代理请求复测通过。
- 公司 DNS 使用隔离的真实打包内核验证备用解析、越界拒绝和公司隧道断开后的保护。公网出站 IPv6 在现有 IPv4 节点环境未验收，隔离内核覆盖 IPv6 合成、目标策略及部分 IPv6-only TLS 样本。AnyConnect UDP 仍按项目的已知历史范围处理。

## 在线更新

- 在临时签名 App 中从 build 77 真实下载、验签、安装到 78，并自动单实例重启。安装结果与 QA 目标的 166 个文件和 9 个链接一致，QA 与正式包应用代码段匹配。
- 篡改 XML 被拒绝，更新包下载次数为零。有效 XML 可在模拟 VPN 活跃时完成下载，草稿只阻止最终安装。开启自动检查的冷启动及无更新返回均允许再次发起模拟连接。
- 在正式 1.0.17 中保留原生更新结果弹窗 60.4 秒。同一进程和 lease 持续有效，观察到 59 次续约和 6 次成功的实际代理 HTTPS 请求。

## 公开回验

四个附件先在 GitHub 草稿核对。发布后匿名确认 stable/latest、tag 对应提交和附件摘要。应用 ZIP、`appcast.xml` 与 `SHA256SUMS` 完整下载并逐字节比对。第三方源码包在本地验证全包与 138 项来源，公开侧核对全包摘要与匿名下载的首 128 KiB。

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.18-macos-arm64.zip` | 32420260 | `ac6cb90297d1dead107e29d03dd8e583ae6eae4f399dbfe64d22423df14c21b8` |
| `GoConnect-1.0.18-third-party-sources.tar.gz` | 216716499 | `782eb04b9bf149f0890b920f949719ee081e8d1380b8b422c55fcac45576acd4` |
| `appcast.xml` | 1471 | `bd43932cffc5e07eab678f8fc0ed7f8e35be459b029938aae7333074eac4075a` |
| `SHA256SUMS` | 287 | `f9c0d94a4b55259aca8c59bbab3786f09702d147907ae42e94ab85942df906d2` |

随后将同一份 XML 快进推送到 `updates`，并从固定 URL 匿名读回。XML 及 ZIP 验签、下载地址、长度、系统要求与 build 均与冻结产物一致。原正式 App 检查到 1.0.18 可下载，关闭提示后再次验证代理可用。

`dist/current/` 保留 1.0.18（78），`dist/rollback/` 保留紧邻的 1.0.17（77）。项目外私有目录 `goconnect-release-1.0.18` 保留构建、实网、OTA 和公开回验回执。
