# GoConnect 2.2.1（build 86）发布记录

2026 年 11 月 27 日，GoConnect 2.2.1 已发布为 GitHub stable/latest。应用内固定更新源已同步并通过匿名回验。维护者安排在自行更新本机后进行实机验收。 [GitHub Release](https://github.com/williamhsu218/goconnect/releases/tag/v2.2.1) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 值 |
| --- | --- |
| 源码提交 | `37d131887db3f49c06dbf29e9b6896e7722075c5` |
| 标注标签 v2.2.1 的 Git 对象 | `22b88fd7ded2b1b2e1cadd930403be6e99302024` |
| GitHub Release ID | `460156702` |
| updates 提交 | `33bf9b6613d39821f11de2de625b0ed688c3700d` |
| 系统要求 | Apple Silicon，macOS 26.0 或更高版本 |
| 应用签名 | Apple Development 团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 沿用既有 Ed25519 公钥，完整 XML 和 ZIP 分别验签 |

## 构建与隔离更新验收

- 48 项 Python 测试和 245 项 Swift 测试通过。Go race、包内 Mihomo 与 OpenConnect 集成检查通过。核对了主程序及内嵌代码的 27 项签名。第三方源码附件包含 141 项来源、203 个归档文件，覆盖内核 `v1.19.34-goconnect1` 的对应源码和补丁。
- 隔离实例从 2.2.0（85）真实下载、验签并安装 2.2.1（86）。应用以新 PID 单实例重启，175 个文件和符号链接与签名目标一致。隔离数据哨兵保留，升级后可再次检查更新。
- 模拟 VPN 连接中可下载并验签。未保存草稿阻止最终安装。冷启动自动检查、关闭更新提示及检查失败后都能继续操作。篡改的更新源在 ZIP 下载前被客户端拒绝。

## 公开核对

草稿的四个附件均核对了名称、大小、GitHub SHA-256 和标签对应的源码提交。Release 公开后，四个附件均匿名完整下载，字节数和 SHA-256 与冻结清单一致。应用 ZIP 和整个 appcast.xml 分别通过 Ed25519 验签。

提升生产源后，客户端固定 URL 匿名读回的 XML 与公开附件逐字节一致。Sparkle 2.10.1 使用 2.2.0 的隔离客户端配置，从固定地址识别到更新。

| 附件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.2.1-macos-arm64.zip` | 36477888 | `da37e748d4bf2d1d7bdf84cdfa7bf92cc1f8433ae3b279269d596a6c48b7324d` |
| `GoConnect-2.2.1-third-party-sources.tar.gz` | 227538001 | `a6fde9dcf06c5f33c9db92ac013cccad6f4cf2a56a1b1e697ae57132bf79c5db` |
| `appcast.xml` | 1494 | `02456ae49bd231a1f21c4c304dc5c9b9d1cf2bcfc8258aabf3076c0e68324fe4` |
| `SHA256SUMS` | 285 | `b774d60b42af68a3f63b0a3ed0616699887686230195e21eed4ea0e4044e39d8` |

## 本机验收与保留包

维护者自行更新后再验收本机 DNS、网页请求与网络服务升级。本轮的已安装环境为 2.2.0（85）。本机保留包暂为 2.2.0 正式版与 2.1.1 回滚版，待维护者确认本机验收后轮转。

AnyConnect UDP 的历史偶发超时沿用项目已接受的非阻断范围。原始证据保存在项目外私有目录 `goconnect-release-2.2.1`。 [发布规范](RELEASING.md)
