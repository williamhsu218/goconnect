# GoConnect 2.1.0（build 83）发布记录

2026-11-22 发布为 GitHub stable/latest。已从应用使用的固定地址匿名读回并验证生产更新源。 [GitHub Releases](https://github.com/williamhsu218/goconnect/releases/tag/v2.1.0) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 结果 |
| --- | --- |
| 设置改版提交 | `957e82df3229049ee1203d7c86c001895333bc5c` |
| 打包源码提交 | `bb59b5c1dc4c29a0dc1558b93d608359c83f163e` |
| 标注 tag 对象 | `89b4fac6662b282fc42607587d174f3d65d846fc` |
| GitHub Release ID | `454421540` |
| updates 提交 | `13ef4c82177ae9908ba33e8b58ed33fc310977aa` |
| 运行要求 | Apple Silicon Mac，macOS 26.0 或更高 |
| 代码签名 | Apple Development 团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 沿用 build 82 公钥，通过完整 XML 和 ZIP 的 Ed25519 验签 |

## 构建与设置窗口

- 团队签名构建通过 47 项 Python 测试、237 项 Swift 测试、Go race 检查、Mihomo 检查和随包网络组件集成测试。需要单独安装服务的 TUN 和公司副连接用例按适用范围跳过。
- 在 macOS 27.2.1 上用正式 ZIP 展开的签名 App 验证登录项注册。退出并重开后，设置开关与系统状态一致。随后取消注册，将登录项恢复为初始关闭状态。
- 正式包的「更新与关于」显示 build 83 和应用更新区，许可入口在 Finder 打开随包 `ThirdParty` 目录。「检查更新」和自动检查状态在团队签名的隔离设置窗口实测。

## 在线更新验收

- 团队签名的 UpdateQA 从 build 82 真实下载、验签并安装到 build 83，随后单实例自动重启。重启后的 166 个文件和 9 个符号链接与签名目标一致，隔离测试数据保留。
- 下载及验签期间模拟 VPN 保持连接，未保存线路草稿拦住安装。释放草稿保护后确认安装，应用正常重启。篡改 feed 被拒绝且未请求 ZIP。启用自动检查后冷启动、取消失败提示及关闭无更新提示，连接开关均可操作。
- 本机正式 2.0.2 的原生更新结果弹窗停留 49.0 秒，主进程、VPN 工作进程和租约持续工作。期间记录 49 次续约，5 次代理请求和关闭后 1 次请求均返回 HTTP 200。
- 生产源放行后，本机正式 2.0.2 已识别 2.1.0，停留在用户确认下载前。该时点通过 VPN 发出的 HTTPS 请求返回 HTTP 200。

## 公开回验与回滚

GitHub 草稿内四个附件的尺寸和 SHA-256 与冻结清单一致。正式发布后匿名完整下载四个附件，验证全部 SHA-256 和 ZIP、XML 的 Ed25519 签名。第三方源码包核对了 138 项来源和 200 个归档文件，与包内组件对应。

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.1.0-macos-arm64.zip` | 32628191 | `0d24c94ef6d650e415589a9135195c91c61d36c67445411f2deae69e789e660f` |
| `GoConnect-2.1.0-third-party-sources.tar.gz` | 216716252 | `a78dfb2e28fa37d4c072a2bc4925d1381cda6032355563e2f6d5600389de2d40` |
| `appcast.xml` | 1535 | `0e800c849aefd389b4d0916dae5a23b9206a9361415a99c330d52a207c07b8c0` |
| `SHA256SUMS` | 285 | `b34addf906bcf4a061b2c3a85e8b7a03d9dcfb090c9b5c3dfecc0bee2a5753ec` |

签名更新源通过快进推送，客户端下载的 XML 与冻结附件逐字节一致。版本、build、最低 macOS 版本、下载 URL、长度和签名均与公开 ZIP 匹配。

`dist/current/` 保留 2.1.0（83），`dist/rollback/` 保留 2.0.2（82）。回滚包中的文件及链接与本机已安装 App 一致。原始验收证据保留在本机私有目录 `goconnect-release-2.1.0`。
