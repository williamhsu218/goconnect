# GoConnect 1.0.19 (build 79) 发布验收

2026-11-20 发布为 GitHub stable/latest，签名生产更新源已公开回验。本机恢复原名单代理后，1.0.17 从应用内查到 1.0.19 可下载。 [GitHub Releases](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.19) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 结果 |
| --- | --- |
| 源码提交 | `d3bc096e1d5e55c431e80b4fd59af87f7372c066` |
| 标注 tag 对象 | `3fa922da5c0ed78bdb17a7ff282db9e0f2167289` |
| GitHub Release ID | `452392781` |
| updates 提交 | `5c8f6d151523f00b921f9102e3713bc5a8d422b6` |
| 运行要求 | Apple Silicon，macOS 26.0 或更高 |
| 代码签名 | Apple Development 团队 `RX44JC39GB`，Hardened Runtime，未经过 Apple 公证 |
| 更新签名 | 既有 Ed25519 公钥，分别验证完整 XML 与应用 ZIP |

## 构建和实网

- 228 项 Swift 测试、4 个 Go 包的 race 检查、打包内核和实际协议集成通过。Sparkle 使用与锁定提交一致的本机缓存，并核对官方签名工具。
- 公司 DNS 回归覆盖 NXDOMAIN、无 A 记录、别名范围、备用优先和 15 秒冷却。冻结正式包的打包内核及公司 DNS 监听器均验证了 UDP／TCP 两种负答复。
- 本机无非链路本地 IPv6 地址。签名独立网络测试版在名单和全局模式均完成 TUN IPv4／IPv6 地址和精确路由、实际系统 DNS、代理 API、直连 HTTPS 及正常断开清理。
- 两种模式均观察到代理 HTTPS、代理 DoH 与原网络 HTTPS 的真实分阶段日志。Chrome 158.0.8127.36 打开公开登录页并加载跨域样式和脚本，名单测试窗口无本地网络权限弹窗。
- 测试服务已卸载。Mac 控制短时失联，正式 1.0.17 首次回连又遇到旧路由缓存。缓存自行过期后恢复原名单线路，正式 App 文件与配置摘要匹配，真实代理请求复测通过。
- 实际 Wi-Fi／有线切换、公司副线实链路和公网 IPv6 留待对应环境实测。AnyConnect UDP 按项目已确认的历史范围处理。

## 应用内更新

- 隔离 App 从 build 78 下载、验签、安装到 79，并自动单实例重启。安装内容与签名 QA 目标的 166 个文件和 9 个链接一致，应用代码段与正式包一致。
- 篡改 feed 被拒绝且未请求 ZIP。模拟 VPN 活跃时完成下载，未保存草稿阻止最终安装。开启自动检查后的冷启动与无更新返回均可重新发起模拟连接。
- 原生更新结果模态使用同一台 Mac、同一已安装 1.0.17 App 和进程的既有 60.4 秒实机回执，正式 App 文件与 PID 已重核。该回执录得 59 次续约与 6 次代理 HTTP 200。本轮对实际生产更新提示另做了重新检查，关闭后主 VPN 同一会话可用。

## 公开回验

GitHub 草稿核对全部四项附件后转为 stable/latest。匿名读回核对 tag、尺寸与摘要。应用 ZIP、XML 和校验清单均完整下载并逐字节比对。第三方源码包本地验证全包和 138 项来源，公开侧以全包摘要和首 128 KiB 匿名下载比对，未重新完整下载。

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.19-macos-arm64.zip` | 32445749 | `a1dfd47c6e1ea896269cd0450d0030fa2d64d53609afae6415f1ce16ab6ed53b` |
| `GoConnect-1.0.19-third-party-sources.tar.gz` | 216716497 | `dc618ef8b6b237d1592c08fd14c925080ceaf5ddd8d5c236ffb3f04c3d353fae` |
| `appcast.xml` | 1464 | `8d8ba481a6d2e70005185e98011dbc45bac51a2e9a1d288af39c46f6524fa358` |
| `SHA256SUMS` | 287 | `5234760c8fefd97e2abf4ae40c19e1d51759810f8a1289b2393c5becccf0bfd1` |

同一份签名 XML 快进推送到 `updates`，经直连和当前 VPN 匿名访问客户端固定地址，所得内容均与签名原件逐字节一致。签名、系统要求、版本及下载地址与冻结附件匹配。

`dist/current/` 保留 1.0.19（79），`dist/rollback/` 保留 1.0.18（78）。原始验收回执保存在项目外私有目录 `goconnect-release-1.0.19`。
