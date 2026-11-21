# GoConnect 2.0.1（build 81）发布记录

2026-11-21 发布为 GitHub stable/latest。签名生产更新源已从客户端固定地址匿名回验。 [GitHub Releases](https://github.com/williamhsu218/goconnect/releases/tag/v2.0.1) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 结果 |
| --- | --- |
| 原网络 HTTPS 修复提交 | `c1344d1b7bb717b61dbabcf43e3e469a5b5a75ff` |
| 打包源码提交 | `8d30b068fde55db4c19784b03321210ce31d7f8b` |
| 标注 tag 对象 | `0e0e9633b52ddedcc2a60ff9a6bf7263d3659d2a` |
| GitHub Release ID | `453646674` |
| updates 提交 | `353f8ed9b917b6587b2325b8a19b43617d896606` |
| 运行要求 | Apple Silicon Mac，macOS 26.0 或更高 |
| 代码签名 | Apple Development 团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 沿用旧版 Ed25519 公钥，分别验证完整 XML 与 ZIP |

## 构建与现场验证

- 234 项 Swift 测试、四个 Go 包的 race 检查、团队签名构建和随包组件的隔离集成测试通过。要求已安装真实 TUN 服务或单独原生前置条件的测试按原门禁跳过。
- 修复前，原网卡到旧探测目标两次 TCP 超时合计约 8 秒。修复后原网 IPv4 实测 148–208 毫秒，IPv6 实测 104 毫秒，代理 HTTPS 和 DoH 同时通过。这一实测在本机活跃 VPN 上运行修复后的独立诊断代码。
- 本机正式 2.0.0 的原生更新结果弹窗停留 48.2 秒，同一主进程、路由进程和租约保持，记录到 45 次续约。弹窗期间 5 次代理请求和关闭后的一次请求均返回 HTTP 200。

## 签名在线更新

- 独立签名 UpdateQA 从 build 80 真实下载、验签并安装至 build 81。应用自动单实例重启，166 个文件和 9 个链接与签名目标逐项一致。
- 模拟 VPN 连接中完成下载。未保存的线路草稿阻止安装，篡改 feed 被拒绝且未请求 ZIP。启用自动检查后冷启动、更新检查失败及关闭无更新提示后均可切换模拟连接。
- 本机正式 2.0.0 已从生产源识别 2.0.1，停在更新提示。正式版安装、网络服务升级和安装后现场验收由用户发起。

## 公开回验与回滚

四个草稿附件的 GitHub 尺寸和摘要匹配冻结清单。发布后匿名完整下载应用 ZIP、签名 XML 和校验清单并核对 SHA-256，应用 ZIP 与 XML 的 Ed25519 签名均通过。第三方源码包本地核对 138 项来源和 200 个归档文件，公开侧核对 GitHub 全包摘要并匿名比对首 128 KiB，公开侧未重新完整下载源码包。

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.0.1-macos-arm64.zip` | 32556003 | `7deb74f03684b329a041d5a03de5a52afe9c7889d8cb1ac7c441a741dac0db8b` |
| `GoConnect-2.0.1-third-party-sources.tar.gz` | 216716220 | `80b2f7f3de3ddc1723bc782888c4041f98a60b47daadd79607e7cfdaa708633c` |
| `appcast.xml` | 1498 | `7f4e274cb73d4b309fd8f5b253354d04623741a9100cf1cc0b57d459320a5ac2` |
| `SHA256SUMS` | 285 | `b33bc78d1871244c900e10ab0c6c6ee0a5a3e7ec8ad9b26b5c1f5e35fcc52d4e` |

同一份签名 XML 快进推送到 `updates`，从客户端固定地址匿名读回的字节、签名、版本、长度和下载地址均与冻结附件一致。

`dist/current/` 保留 2.0.1（81），`dist/rollback/` 保留 2.0.0（80）。发布前核对本机 App 与签名回滚包一致，配置和流量数据库已另作私有备份。原始验收证据保存在项目外私有目录 `goconnect-release-2.0.1`。
