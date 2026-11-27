# GoConnect 2.2.2（build 87）发布记录

GoConnect 2.2.2 于 2026 年 11 月 27 日发布为 GitHub 正式版。生产更新源已同步，2.2.1 隔离客户端从固定 URL 检出更新。 [GitHub Release](https://github.com/williamhsu218/goconnect/releases/tag/v2.2.2) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 值 |
| --- | --- |
| 功能提交 | `9c0ce284cb32e2b7afcc5ee716f96748b840176e` |
| 正式包源码提交 | `bce6efe159a4addb841a48ccf3732f62b68dbfb0` |
| 标注标签 v2.2.2 对象 | `d246e9886afb5efa269ae4ac105f57822f032ef8` |
| GitHub Release ID | `460796058` |
| updates 提交 | `bf4eb3d9cbbd831a98e16f15b0e20c8e30388ecd` |
| 系统与架构 | Apple Silicon，macOS 26.0 或更高版本 |
| 签名 | Apple Development，团队 RX44JC39GB，Hardened Runtime，未经过 Apple 公证 |

## 发布门禁

- 四个功能文件的 SHA-256 与发布前验收记录一致。正式包内 27 项签名通过核验，stable 配置、既有 Ed25519 公钥和强制验签设置与现有客户端兼容。
- 48 项 Python 测试、245 项 Swift 测试、Go 竞态检查及包内集成测试通过。首轮全局模式的 UDP 目的地址隔离测试出现一次回环读取超时。该用例在已安装内核和最终包内内核上分别连续通过 12 轮，同一提交完整重跑通过。复测使用原有断言和超时。
- 从冻结 ZIP 取出的 GoConnectTransport 和 Mihomo 经过正常文件导入与连接入口，在 IPv4 回环上完成 VLESS + REALITY 的 TCP + Vision 和 gRPC 实连。两种传输均返回 HTTP 200 和预期随机正文。仅改错短 ID 后，两次请求均未到达源站。
- Sparkle 隔离客户端从 2.2.1（86）实际下载、验签并安装 2.2.2（87），以新 PID 单实例重启。175 个文件和符号链接逐项匹配，隔离数据哨兵保留。模拟 VPN 可保持连接完成下载和验签，未保存草稿会阻止安装。等长篡改的更新源在请求 ZIP 前被拒绝。
- 第三方来源包覆盖 141 项来源和 203 个归档文件，125 个 Go 模块逐项复核 h1，Mihomo 锁文件与补丁、Sparkle 源码及许可证均与本次应用包匹配。

## 公开附件

草稿的四个附件均核对了名称、尺寸、SHA-256 和对应标签提交。Release 公开后再次检查 stable/latest、全部附件及 Ed25519 签名。四个附件均经匿名完整下载，逐字节与冻结包比对一致。固定生产 URL 返回的 XML 与冻结附件逐字节一致。

| 附件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.2.2-macos-arm64.zip` | 36482660 | `41aeadb621af0b490fc02bd89aee35da92a099ff5c1203b593e5685d5364e5db` |
| `GoConnect-2.2.2-third-party-sources.tar.gz` | 227537969 | `571eb58f1511828315fefe3405691ca1b8bedd040c6c0f178afb116c237591b6` |
| `appcast.xml` | 1568 | `b16516c22b7f041cd8efc6b603e2362258ba2a92d6eeec21e3227066ed003fb6` |
| `SHA256SUMS` | 285 | `7783064284e06272fcaf27049957b3369bb70a84d67926743b625a1a2eb64fb3` |

## 本机状态

本机仍运行 2.2.1（86）。维护者手动更新后，再进行已安装正式版与实际线路的本机验收。已签名的 2.2.2 ZIP 和对应校验值已在项目外留存。当前项目保留的正式／回滚包仍为 2.2.0／2.1.1，待本机验收确认后轮转至 2.2.2／2.2.1。前一正式版 2.2.1 的 ZIP 已核对公开 SHA-256。

AnyConnect 公司副连接延续 IPv4 / TCP 的使用范围。历史 UDP 偶发超时沿用已接受的非阻断记录。脱敏验收回执在项目外私有目录 `goconnect-release-2.2.2`。 [发布规范](RELEASING.md)
