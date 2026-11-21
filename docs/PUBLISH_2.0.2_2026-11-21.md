# GoConnect 2.0.2（build 82）发布记录

2026-11-21 发布为 GitHub stable/latest，签名生产更新源已从客户端固定地址匿名回验。 [GitHub Releases](https://github.com/williamhsu218/goconnect/releases/tag/v2.0.2) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 结果 |
| --- | --- |
| 菜单栏优化提交 | `3ef709bcd4fd6f96bf756395c57b2ade477c8212` |
| 打包源码提交 | `a11c6dd2a0c370cfcbdae31608a016258253138a` |
| 标注 tag 对象 | `b72a2496d4783823e58f4ed7ca22e2663730d725` |
| GitHub Release ID | `453704578` |
| updates 提交 | `739576494baa15e3eaff98beef28b608ecad24eb` |
| 运行要求 | Apple Silicon Mac，macOS 26.0 或更高 |
| 代码签名 | Apple Development 团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 沿用 build 81 公钥，分别验证完整 XML 与 ZIP |

## 构建与菜单栏

- 234 项 Swift 测试、四个 Go 包的 race 检查、团队签名构建和随包组件的隔离集成测试通过。要求已安装真实 TUN 服务或单独原生前置条件的部分用例按原门禁跳过。
- 首轮隔离 DNS 用例遇到本地 SOCKS 端口拒绝连接，重新执行完整团队签名构建和验证后通过。
- 隔离原生菜单栏测试覆盖单双行切换、图像复用、悬停与辅助功能的完整数值、数据过期及恢复图标模式。两轮使用相同的 1 Hz 速率序列，测试进程平均 CPU 从 0.69% 降至 0.19%。本机安装 2.0.2 后的后台 CPU 由用户手动验收。

## 签名在线更新

- 独立签名 UpdateQA 从 build 81 真实下载、验签并安装到 build 82，随后自动单实例重启。重启后的 166 个文件和 9 个链接与签名目标一致。
- 模拟 VPN 连接中下载成功，未保存线路草稿阻止最终安装，篡改 feed 被拒绝且未请求 ZIP。启用自动检查后的冷启动、失败取消和关闭无更新提示均可切换模拟连接。
- 本机正式 2.0.1 的原生更新结果弹窗停留 48.3 秒，同一主进程、路由进程和租约持续工作，记录到 47 次续约。弹窗打开期间 5 次代理请求、关闭后一次代理请求均返回 HTTP 200。
- 本机 2.0.1 已从生产源识别 2.0.2，停在更新提示，下载和正式安装由用户确认。

## 公开回验与回滚

四个草稿附件的 GitHub 尺寸和摘要与冻结清单匹配。发布后匿名完整下载 ZIP、签名 XML 和校验清单，核对 SHA-256 并验证 XML 与 ZIP 的 Ed25519 签名。第三方源码包在本机核对 138 项来源和 200 个归档文件，公开侧以 GitHub 全包摘要和首 128 KiB 的匿名 Range 下载回验，公开侧未重新完整下载该源码包。

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.0.2-macos-arm64.zip` | 32556825 | `f8a04caa4b572867edad1cfd0bb6c1d66056f9e4a19c71acc9122dfc78d57b0a` |
| `GoConnect-2.0.2-third-party-sources.tar.gz` | 216716243 | `641611769009569dde3afc122dd10d77bada955ca387f36fb0f06fee843526b1` |
| `appcast.xml` | 1486 | `17bd5428bbd31617cc70aac5d069f5f914b7650cffcb947e82aa009fb40b597b` |
| `SHA256SUMS` | 285 | `0570745732409cda9b08b87a7dc389b1941a29f73b680489401d057c28f3ebf0` |

同一份签名 XML 快进推送至 `updates`。客户端固定地址读回的 XML 字节、签名、版本及 enclosure 元数据与冻结附件一致。

`dist/current/` 保留 2.0.2（82），`dist/rollback/` 保留 2.0.1（81）。发布前核对已安装 2.0.1 与签名回滚包一致，配置及流量库已另作私有备份。原始验收证据在项目外私有目录 `goconnect-release-2.0.2`。
