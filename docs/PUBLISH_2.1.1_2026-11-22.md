# GoConnect 2.1.1（build 84）发布记录

2026-11-22 发布为 GitHub stable/latest，签名生产更新源已从应用使用的固定地址匿名回验。 [GitHub Releases](https://github.com/williamhsu218/goconnect/releases/tag/v2.1.1) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 结果 |
| --- | --- |
| 焦点圈修复提交 | `d1cc4d6652b54a123c103f3e0dc2f2eefc991307` |
| 打包源码提交 | `44e7db3d5d95e1d31ae7c7b856a56d74f96d0221` |
| 标注 tag 对象 | `a30975f7f7829b346b5fc3b88f1f1e54d5541305` |
| GitHub Release ID | `454446260` |
| updates 提交 | `0fecd1290d78f8729e31ed4967a2e5ccd854e2e5` |
| 运行要求 | Apple Silicon Mac，macOS 26.0 或更高 |
| 代码签名 | Apple Development 团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 沿用 build 83 公钥，完整 XML 与 ZIP 分别通过 Ed25519 验签 |

## 构建与设置验收

- 团队签名构建通过 47 项 Python 测试、237 项 Swift 测试、Go race 检查、Mihomo 检查与隔离集成测试。需要已安装服务的 TUN 和公司副连接专项用例按本版适用范围跳过。
- 在团队签名的 build 84 UpdateQA 中，关闭并重开设置与鼠标点击标签时只显示选中底色。首次 Tab、Shift+Tab 和方向键导航显示焦点圈，鼠标再次点击同一标签可隐藏焦点圈。逐帧检查捕获了选中胶囊滑动的中间位置。

## 签名在线更新

- 团队签名 UpdateQA 从 build 83 真实下载、验签并安装到 build 84，随后自动重启为单实例。重启后的 166 个文件和 9 个符号链接与签名目标一致，隔离测试数据保留。
- 模拟 VPN 连接和未保存草稿期间可下载，草稿保护拦住最终安装。篡改 feed 被拒绝且客户端未请求 ZIP。取消失败提示、开启自动检查后冷启动、关闭无更新提示后，连接开关均可继续操作。
- 本机正式 2.1.0 原生更新结果弹窗停留 49.0 秒，主进程、VPN 工作进程和租约持续运行，记录到 49 次续约。弹窗期间 5 次代理请求及关闭后的请求均返回 HTTP 200。
- 生产源放行后，本机 2.1.0 已显示 2.1.1 更新提示，继续由用户确认下载和安装。此时通过 VPN 的 HTTPS 请求返回 HTTP 200。

## 公开回验与回滚

四个 GitHub 草稿附件的尺寸及 SHA-256 与冻结清单一致。正式发布后匿名完整下载并核对四个附件、应用 ZIP 和完整 feed 的 Ed25519 签名。第三方源码包核对了 138 项来源和 200 个归档文件。

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.1.1-macos-arm64.zip` | 32635937 | `15fe3c9cfebccd5855c9d694945aaf86cfa1b789a5745313590fb314c8416840` |
| `GoConnect-2.1.1-third-party-sources.tar.gz` | 216716398 | `1fc02abddd868891b71395dc7d2f2a11066ce3ffe580b4be6abd4e929bbc0b42` |
| `appcast.xml` | 1513 | `7648fc702e3688366dce03d10e50ffd6ac8939ac4ad23bc8a2729ed96c3e32ad` |
| `SHA256SUMS` | 285 | `e3832290d90e77ff1ddae0fbd0e81f4cba48a5c301836724518e9e4430a6672c` |

同一份签名 XML 已通过快进推送到 `updates`。固定 URL 读回的字节、版本、build、最低系统、下载地址、长度和签名与已公开附件一致。

`dist/current/` 保留 2.1.1（84），`dist/rollback/` 保留 2.1.0（83）。回滚包中的文件和链接与本机已安装版一致。原始证据留在项目外私有目录 `goconnect-release-2.1.1`。
