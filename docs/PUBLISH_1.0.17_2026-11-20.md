# GoConnect 1.0.17 (build 77) 发布与验收

2026-11-20 发布为 GitHub stable/latest。应用内生产更新源已同步并完成匿名读回。当前 Mac 的正式 GoConnect 仍运行 1.0.16（76），本次仅安装独立验收副本。 [GitHub Releases](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.17) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

## 版本与签名

| 项目 | 结果 |
| --- | --- |
| 构建源码 | `28170362967f567018f4f2dcd551724a0a1e092b` |
| 标注 tag | `v1.0.17`，tag 对象 `c6e2b1482f173b91e85bdfc1b448258b56b271be` |
| GitHub Release ID | `452183377` |
| updates 提交 | `c7f6db2b8ab349dbb30e6c5dee424e2d126209ef` |
| 系统要求 | Apple Silicon，macOS 26.0 或更高 |
| 分发签名 | Apple Development，团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 既有 GoConnect Ed25519 公钥，stable，完整 XML 与 ZIP 分别验签 |

## 验收

- 团队签名构建通过 226 项 Swift 测试、4 个 Go 包的 race 测试和隔离运行检查。重新展开冻结的正式应用 ZIP 与 QA ZIP 后，签名和嵌套组件校验通过。此前临时展开目录缺少框架资源，验收改用新展开的副本。
- 代理 HTTPS、代理 DoH 和绑定原网络出口的 HTTPS 均完成实际请求。切网处理通过路径变化、原会话清理、清理失败时阻止恢复、同线路重试和取消的隔离回归。原生路径监测在本机当前网络完成稳定判定。批量上传期间一次代理抽测发生 TLS 超时，直连抽测正常。暂停大文件上传后，三项复测通过。
- 独立测试 App 从 76 识别并下载 77，Sparkle 在解包前验签，确认后安装并自动单实例重启。安装结果的 166 个普通文件和 9 个符号链接与 QA 目标完全一致；独立包与正式包的程序代码段一致。
- 篡改 feed 被客户端拒绝，未触发 ZIP 请求。模拟 VPN 连接中可完成下载，草稿阻止最终安装，解除草稿后才正常交接。开启自动检查后冷启动和无更新返回均可重新发起模拟连接。
- 已安装的 1.0.16 原生更新结果弹窗持续采样 60.2 秒。同一进程及 lease 保持，记录 60 次不同的续约时间，6 次代理 HTTPS 均返回 200，关窗后代理请求仍返回 200。正式 App 文件、用户配置摘要和当前主 VPN 会话与验收前一致。

## 公开回验

GitHub 草稿的四个附件全部核对后转正式发布。匿名确认 tag 指向上述源码、Release 为 stable/latest。应用 ZIP、`appcast.xml` 和 `SHA256SUMS` 完整下载且与冻结字节一致。第三方源码包在本地验证全包与 138 项来源记录，公开侧核对 GitHub 全包 digest 和匿名下载的首 128 KiB。

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.17-macos-arm64.zip` | 32415539 | `2b2593eb08b399b465090a0bac9117811053bb7ee728cbba51b81776b0f27bcc` |
| `GoConnect-1.0.17-third-party-sources.tar.gz` | 216716397 | `fc77a031cf04124eaee069989729d6f20c3904f7cd3defd85d177c31683528d2` |
| `appcast.xml` | 1537 | `995b78ae57c4ac663c2654bcf38a1d16781dd13884a0629da9c4d9bba9b52d9b` |
| `SHA256SUMS` | 287 | `9ee54ae222dfdcace85960b32307d1f1fa9ae651598e9ef9591ff785659bd49c` |

发布附件回验通过后，将同一份 XML 快进推送至 `updates`。客户端固定 URL 匿名读回字节一致，版本、build、stable、最低系统、ZIP 长度和 Ed25519 签名均匹配。同一地址也通过 macOS URLSession 匿名读回和验签。在正式 1.0.16 中实际检查到 1.0.17 可下载，关闭提示后主线继续运行。应用内检查和下载可保留现有连接。确认安装重启后按正常流程断开，再按「诊断」提示升级网络服务。

## 保留与实测范围

`dist/current/` 保留 1.0.17（77），`dist/rollback/` 保留紧邻的 1.0.16（76），两包均通过保留检查。实际 Wi-Fi／有线切换和公司副线随主线恢复尚未实测。项目外私有验收目录 `goconnect-release-1.0.17` 保留构建、OTA、模态连接、签名和公开回验回执。
