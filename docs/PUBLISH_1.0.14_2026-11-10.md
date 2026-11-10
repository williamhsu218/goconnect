# GoConnect 1.0.14 发布与验收记录（2026-11-10）

**发布完成：GitHub stable/latest、四个附件、完整 feed 与 ZIP 签名及固定生产更新地址均已公开回验。用户本机已安装同一份 build 74，并恢复原名单代理主线，公司副连接关闭。**

## 源码与信任链

- 源码/构建提交 `787df2804e6f65b39e513c97d27a23b19285d318`；annotated tag 对象 `1da32a8d64459be9179151561a5cd483089e2b9b`。发布 tag 固定已验收构建提交，发布记录和当前版本说明另同步至默认分支，未改写 tag。
- 从已验收 1.0.13 源码演进，没有带入独立开发分支尚未验收的地址导入功能；默认分支同步合并该已验收修复。
- 沿用团队 RX44JC39GB 的 Apple Development + Hardened Runtime、生产 Bundle ID 和既有 Ed25519 更新账户/公钥。不是 Developer ID 分发签名，未经过 Apple 公证；Apple Silicon / macOS 26.0 及以上。
- stable、固定 HTTPS feed、完整 feed 验签及 ZIP 解包前验签保持开启；自动下载/安装关闭，自动检查默认关闭，保留用户选择。helper CDHash、授权及退出边界未放宽。
- Sparkle 2.10.1 官方 SDK 缓存 ZIP 的尺寸与 SHA-256 均匹配官方 Release digest：10197413 字节，`4c2e48a3e50ebcc2c88a58a4c7264a1a99c3c31857ce754357afb9cdb323022a`。

## 修复与本机验收

- 本版仅调整名单模式的合成 AAAA 答复，避免虚拟 ULA 使公网网站触发浏览器本地网络权限。全局代理 DoH、内网真实 AAAA、IPv6 转发与按应用分流保留。详见 [实现及原生记录](DNS_BROWSER_COMPATIBILITY_2026-11-09.md)。
- 对原配置的真实内核回归先失败，修正后通过；团队签名 `--verify` 通过 39 项 Python、194 项 Swift、适用 Go race、包内内核及打包集成检查。显式安装服务/TUN 专用测试与部分旧路径测试的跳过不计为通过。
- 2026-11-09 授权后正常安装、升级服务并重连。安装后 172 个文件/符号链接与冻结 ZIP 相同，特权组件映像哈希与包内组件一致。Chrome 156.0.8078.80 对全新站点来源的公网请求不再弹窗，真实内网请求仍受权限保护；未修改浏览器权限。
- 名单与全局系统 DNS、AI API 证书验证及未登录 401、国内与内网只读请求通过；权威侧对照和原生 IPv6/IPv4-mapped 解析按记录验证。未将 API 传输成功解释为登录后业务功能验收。
- 原生更新模态弹窗保持超过 45 秒；连续采样 60.341 秒，7 个进程与原 lease 身份不变，55 次续约，最大年龄 1.036 秒，6 次代理请求全部 200；关闭后仍正常。
- 全局对照后因 Mac 锁定暂留全局；2026-11-10 用户解锁后正常恢复原名单主线并复查通过。完整配置与安装前 JSON 一致，订阅哈希未变。公司副连接全程关闭，本轮没有重新进行公司服务器认证。

## 隔离真实在线更新

- 本轮独立 UpdateQA 从 build 73 实际下载、验签、安装并自动重启到 74，PID 53942 → 54695，连续 12 个 0.2 秒样本为单实例。
- 安装后 163 个普通文件、9 个符号链接与目标 QA 包完全一致，严格签名检查通过；生产 ZIP 从未为 QA 改写，两份 QA 的可执行文本与各自生产源一致。
- 篡改 feed 被实际客户端拒绝，拒绝阶段 ZIP GET 为 0。合法下载时模拟连接与未保存草稿保持，草稿阻止最终安装；明确清除草稿并确认后才正常结束模拟连接并安装。
- 新 build 可重新连接；开启 QA 自动检查后冷启动、无更新结果及失败恢复均未残留连接锁。QA 偏好恢复原值。模拟连接只证明更新状态流程，生产连接保护另由前述真实模态验收覆盖。
- 发布阶段当前正式 App 与网络组件进程、配置和订阅均未改变；没有为发布重连主/副 VPN。

## 冻结附件与公开回验

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.14-macos-arm64.zip` | 32137805 | `5f52b437ea2ae9e45d17f5a1d785f6a2431ca8d5be8511fe2078d078852b6fe2` |
| `GoConnect-1.0.14-third-party-sources.tar.gz` | 216709249 | `5078c252deeec001eeeb31f057f95ba16cbdfc04d8e712445ddbed81a3cefae5` |
| `appcast.xml` | 1583 | `2cc4cc13f5b205f747fd757318616e71acc2dacc68b15899fe91777a3e9671e7` |
| `SHA256SUMS` | 287 | `ea27474072de35e5ffabefab3d6ac5d309240d3952a6c8354b192d9ccff151c0` |

四附件先上传 draft，核对 uploaded 状态、尺寸、完整 digest、tag 源码后公开为 [1.0.14 正式 Release](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.14)，Release ID 439816526，stable/latest 正确。匿名完整下载 ZIP、XML、SHA256SUMS 并逐字节比对，官方 SDK 回验 XML 与 ZIP 的 Ed25519 签名及 enclosure 元数据。

第三方源码对应冻结 App，本地审计 138 条来源、122 个 Go 模块 h1 和全部归档 payload 哈希，tar 共 215 个成员。公开回验采用 GitHub 全包 digest 与匿名 128 KiB Range 比对，未重新完整下载源码包。

## 签名生产更新源

公开附件回验通过后，fast-forward 更新 updates：`3c99a94f35a6c8aa1ac5512a5f5fbac198cb3886`，父提交 `4f6df1ecdfb6b21408599d9b036c6558d2e5dbd1`。提升前重新验签旧 build 73，确认没有并发更高 feed。

固定 [客户端原 URL](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml) 经匿名 curl（现有主 VPN 的临时本地中转）与原生 URLSession 读回同一份 1583 字节，SHA-256 `2cc4cc13f5b205f747fd757318616e71acc2dacc68b15899fe91777a3e9671e7`；build 74、stable、enclosure 和整个 XML 签名正确。原网络匿名 API 起初触发 GitHub 限流，直接 curl 的某个 CDN 缓存也短时保留旧 feed；未使用随机查询参数、更换固定 URL、覆盖附件、重建钥匙或 force push。最终 VPN curl 与原生 URLSession 的固定地址回验均通过。

## 保留与已知边界

已保留 current 1.0.14 / 74、rollback 1.0.13 / 73，各一个 ZIP 和 SHA256SUMS，清单检查通过。本轮临时 QA、下载、SDK、源码和 worktree 在核对后清理，验收证据保留在项目外私有目录 `~/.codex/visualizations/2026/11/10/goconnect-release-1.0.14/`，先前安装证据另在 `~/.codex/visualizations/2026/11/09/goconnect-browser-dns-fix/`。

仅接受原始 AAAA 而不接受 IPv4 映射的专用客户端在名单延迟解析区域仍有限制；应用自带 DoH、独立 DNS 与 Finder 内核 SMB 按各自边界处理。公司功能仍限明确 IPv4/TCP；历史 AnyConnect UDP 继续按已接受的非阻断限制处理，没有重新完成该稳定性验收。
