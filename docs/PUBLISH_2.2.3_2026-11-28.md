# GoConnect 2.2.3（build 88）发布记录

2026 年 11 月 28 日发布为 GitHub 正式版。生产更新源已同步。 [GitHub Release](https://github.com/williamhsu218/goconnect/releases/tag/v2.2.3) · [生产更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)

| 项目 | 值 |
| --- | --- |
| 构建提交 | `1f207ed09919c805e4bdebf3c94a0705e13f28d4` |
| 标注标签对象 | `ef0c338bf8d9d4734530773c73d0f70933d09028` |
| 更新源提交 | `1cac206b25baa750dbd6395da7386020c29beacd` |
| GitHub Release ID | `460956892` |
| 签名与系统要求 | Apple Development 团队 `RX44JC39GB`，Hardened Runtime，Apple Silicon，macOS 26.0 及以上 |

## 验收

- 48 项 Python 测试、245 项 Swift 测试、Go 竞态检查和最终签名包的集成测试通过。Mihomo 构建前测试覆盖普通 TCP 与 TFO 建连前的接收缓冲回读。
- 从冻结 ZIP 启动包内导入组件和内核。经物理网卡，四个测试节点均取得 HTTP 206 并读取相同的 65,536 字节。四个外层 TCP 套接字均读回 8,388,608 字节。省略选项的节点仍可完成同一请求，并按默认缓冲运行。
- 隔离 Sparkle 客户端从 2.2.2（87）实际下载、验签、安装 2.2.3（88），并以新 PID 单实例重启。目标包的 175 个文件与符号链接逐项一致。模拟 VPN 在下载和验签期间保持连接，未保存草稿阻止安装，篡改的签名更新源在下载 ZIP 前被拒绝。
- 从最终包生成的第三方源码附件包含 203 个文件。Mihomo 上游归档、补丁与内嵌锁文件对应。发布包只包含通用配置能力。

## 公开回验

草稿上传后核对四个附件的尺寸和 SHA-256，并核对标签解析后的构建提交。正式发布后确认此版为 latest。四个附件均已匿名完整下载并与冻结文件逐字节核验。固定生产 URL 返回与冻结附件一致的 XML，更新源与其指向的应用 ZIP 均通过 Ed25519 验签。

| 附件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-2.2.3-macos-arm64.zip` | 36484475 | `dfd02759dd453598d169dc8f8cdf76e12c0b8e7b7dda9666efd5a769d43e1c28` |
| `GoConnect-2.2.3-third-party-sources.tar.gz` | 227538895 | `6d55c5f572ee5491c3a8d80417aac5e5a52c495468a9908af30b95225ac07647` |
| `appcast.xml` | 1461 | `0fa932c9578845eee3397f21ec47b4cc77a09d3b6862a0b78aac6d4eb1931f51` |
| `SHA256SUMS` | 285 | `c2e8436e88daad1e7e09278857963bcfb282d07cbe3276de45174d61b95ab655` |

## 本机状态

发布时本机应用为 2.2.2（87）。正式安装及本机在线验收由维护者手动更新后进行。冻结 ZIP 和脱敏回执保存在项目外私有目录 `goconnect-release-2.2.3`。本地正式／回滚包等待本机验收确认后轮转。

沿用 Apple Development 团队签名，尚未经过 Apple 公证。AnyConnect 公司副连接沿用 IPv4 / TCP 的使用范围，UDP 已知偶发超时的验收边界见发布规范。 [发布规范](RELEASING.md)
