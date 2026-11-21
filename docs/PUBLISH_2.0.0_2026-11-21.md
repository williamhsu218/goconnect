# GoConnect 2.0（build 80）发布记录

2026-11-21 发布为 GitHub stable/latest，签名生产更新源已公开回验。 [GitHub Releases](https://github.com/williamhsu218/goconnect/releases/tag/v2.0.0) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 结果 |
| --- | --- |
| 源码版本 | 2.0.0（build 80） |
| 源码提交 | `19a4312de26997769b119fec6b741ea367e95803` |
| 标注 tag 对象 | `d6424cbf12f9db3485e61fc20ea4d2df2c1efdac` |
| GitHub Release ID | `453590305` |
| updates 提交 | `6486a1780880ec8d63352f8dacb4631ad52bc02d` |
| 运行要求 | Apple Silicon Mac，macOS 26.0 或更高 |
| 代码签名 | Apple Development 团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 沿用既有 Ed25519 公钥，分别验证完整 XML 和 ZIP |

## 构建与界面

- 234 项 Swift 测试、4 个 Go 包的 race 检查、签名构建和随包组件的隔离集成测试通过。带真实已安装服务、原生签名子进程或特定出站环境前置条件的部分测试跳过。
- macOS 隔离 QA 覆盖页面切换、草稿保护、前后台呼吸灯、单年份流量悬停、提示倒计时和节点卡片对齐。此轮实网核验覆盖已安装 1.0.19 的更新弹窗和下载期间的主 VPN。安装 2.0 后的现场使用由用户手动验收。

## 在线更新

- 独立签名 UpdateQA 从 build 79 真实下载、验签并安装至 build 80，自动单实例重启。重启后 166 个文件和 9 个链接与签名目标逐项一致。
- 模拟 VPN 连接中完成下载，未保存线路草稿成功阻止最终安装。篡改 feed 被拒绝且未请求 ZIP。开启自动检查并冷启动后可发起模拟连接，无更新提示关闭后连接仍可切换。
- 本机正式 1.0.19 在原生更新结果弹窗停留 50.6 秒期间保留同一主进程和同一网络租约，记录到 50 次续约及 5 次代理 HTTP 200。
- 本机正式版已识别 2.0，下载和验签完成，停在「安装并重启应用」。交接时仍运行 1.0.19，VPN 在线且租约相同。安装和使用验收由用户发起。

## 公开回验和回滚

全部四个草稿附件通过 GitHub 尺寸和摘要检查，随后发布为 stable/latest。应用 ZIP、XML 和校验清单经完整匿名下载，与冻结原件一致。第三方源码包完成 138 项本地来源核验，公开侧核对 GitHub 全包摘要并匿名比对首 128 KiB。公开侧没有重新完整下载源码包。

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.0.0-macos-arm64.zip` | 32553938 | `01467e818c98c250b185ca5fac818b6d5394d4a62ecf813e4daa98e46aaed40b` |
| `GoConnect-2.0.0-third-party-sources.tar.gz` | 216716142 | `5782cd7c10aca05a29501a6b0e2c24a2cd636c3fe747faf1ca63c35b471a8d51` |
| `appcast.xml` | 1513 | `fde2319245f3d6fdb7dda356b4dfacd0f5e23f53af2ac6ec98bedc9250e304b5` |
| `SHA256SUMS` | 285 | `19a9a3dd00bdf2f01c1b0650d6a795d1b9a744215ac1136479ec022657aba2cd` |

公开 ZIP 与签名 XML 通过 Ed25519 回验。生产 feed 采用同一份签名 XML，快进推送后从客户端固定 URL 匿名回读，字节、签名和 enclosure 元数据一致。

`dist/current/` 保留 2.0.0（80），`dist/rollback/` 保留 1.0.19（79）。本机安装前的 App 文件与回滚包一致，配置和流量数据库已另作私有备份。原始验收证据保存在项目外私有目录 `goconnect-release-2.0.0`。
