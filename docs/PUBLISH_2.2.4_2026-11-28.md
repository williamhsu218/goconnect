# GoConnect 2.2.4（build 89）发布记录

2026 年 11 月 28 日已发布为 GitHub 正式版，签名生产更新源已同步。 [GitHub Release](https://github.com/williamhsu218/goconnect/releases/tag/v2.2.4) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 值 |
| --- | --- |
| 修复提交 | `6f76a8fb64bdee3f775244690beb73073e41fb03` |
| 构建提交 | `3f4dae163d6856d13873b3d00a1d5a51f2b0ebd1` |
| 标注标签对象 | `54dca3a346fd15e81fa8c30536a0bf8045492414` |
| 更新源提交 | `2784e4f1a247d369501b6c18f5c03f7948c89691` |
| GitHub Release ID | `461540520` |
| 平台与签名 | Apple Silicon，macOS 26.0 及以上，Apple Development 团队 RX44JC39GB，Hardened Runtime |

## 验收

- 团队签名构建通过 48 项 Python 测试、247 项 Swift 测试、Go 竞态检查及包内运行组件的集成测试。首次检查因下载上游源码时 TLS 连接中断而退出，重新执行完整检查通过。
- 冻结包中的 VLESS + REALITY 主线路在公司副连接启用前、使用中及阻断后均能访问测试网页。隔离测试核对了已有连接和新连接，公司域名和私网 IP 仅在公司通道启用时可达。打包的 OpenConnect 也通过本地公司角色的认证、CSTP TCP 传输与退出清理测试。
- Sparkle 隔离客户端从 2.2.3（88）识别并下载 2.2.4（89），完成验签、安装和单实例重启。已核对更新后文件及符号链接，验证模拟 VPN 下载保护、未保存草稿的安装保护及篡改更新源的拒绝行为。
- 已从最终 App 生成对应第三方源码附件并核对来源归档、模块校验值、Mihomo 锁文件与补丁。正式包、更新源及隔离更新客户端沿用既有团队和更新公钥。当前签名未经过 Apple 公证。

## 公开回验

草稿阶段核对了标签提交、四个附件的名称、尺寸和 SHA-256。公开后确认 stable/latest，匿名取回四个附件的全部字节并逐项比对。第三方源码包的单次下载超时后，保留已下载的前段，以 HTTP Range 取回其余字节，完整重组后 SHA-256 和逐字节比对均通过。固定生产 URL 返回与冻结附件相同的 XML，整个更新源及其指向的 ZIP 均通过 Ed25519 验签。

| 附件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.2.4-macos-arm64.zip` | 36484756 | `fed5571fc165d64cf96aea88a4bd8e2a083f59a7225456f70aff50d4d948b7f6` |
| `GoConnect-2.2.4-third-party-sources.tar.gz` | 227539122 | `08ff3207ac85017df7413daa84d4ff134cb3cb55dfc0f08b38789a7a83e0d176` |
| `appcast.xml` | 1519 | `75154cca5c70c306ca3b86c6fd23bea690eee6b4ac49513e21f49a4136a15961` |
| `SHA256SUMS` | 285 | `31d6225ffb6a136d0e8bedf93242501b1f3321e89248319ef555367a07715a10` |

## 本机状态

发布时本机仍运行 2.2.3（88）。维护者将在手动更新后使用真实公司账号验收公司网络及系统 TUN 的分流。公司业务目前适用于 IPv4 / TCP，AnyConnect UDP 的历史限制见发布规范。

冻结 ZIP 和脱敏回执保存在项目外私有目录 `goconnect-release-2.2.4`。本地正式／回滚包保持 2.2.3／2.2.2，待维护者完成实机验收后轮转。 [发布规范](RELEASING.md)
