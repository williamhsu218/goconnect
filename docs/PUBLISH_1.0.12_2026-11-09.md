# GoConnect 1.0.12 发布与验收记录（2026-11-09）

GitHub stable/latest、四附件和签名生产更新源已公开回验。本机安装 1.0.12 / build 72，正常升级网络服务，恢复原名单代理线路；公司副连接保持关闭。后续检查另发现原有模态弹窗心跳问题，见末节，安排 1.0.13 修正，不覆盖本版 tag 或附件。

源码提交 `d8185c1079fefaf86ceccc7ed68a649c29dc9500`，tag 对象 `b86f0c899fb82b900c6626c428e8ded3c8cf7f70`。从已验收主线单独纳入 socket 清理与名单 DNS 修复，未包含工作分支上尚未验收的直连地址导入功能。

## 构建与分流验收

- 团队 RX44JC39GB 的 Apple Development + Hardened Runtime；正式 Bundle ID、stable、既有公钥及固定 HTTPS feed 保持一致。完整 feed 与 ZIP 解包前验签开启。不是 Developer ID 分发签名，未公证；最低 macOS 26.0 / Apple Silicon。
- `--verify --team-sign` 退出 0，247.947 秒；39 项 Python、193 项 Swift、适用 Go race、真实内置 OpenConnect/CSTP 与隔离核心检查通过。专用原生门禁的自动跳过不计为已安装验收。
- 额外使用冻结包内 Transport/OpenConnect 在相同 Unix socket 路径完成四轮 CSTP TCP 回归，其中两轮 SIGKILL 本轮 hook，随后正常结束其所属进程组。每轮 socket 均移除并可再次绑定；保留活跃、替换或身份不明文件的单元回归通过。测试服务器为隔离 TLS fixture，不是本轮公司服务器实网认证。
- 经用户授权维护窗口后才断开、安装及升级服务。安装的 172 个普通文件/符号链接与冻结 App 一致；服务 Transport SHA-256 一致，原生 TUN IPv4/IPv6 地址与映射地址路由读回正确。
- 名单外原生 App 的公网出口保持物理网络；公司域名 HTTPS 从旧版 403 恢复 200，局域网 HTTP 服务 200。真实浏览器回测页与历史页成功加载；仅导航读取，没有生成、购买或编辑业务数据。
- 将同一签名探针临时加入名单后，公网出口匹配主 VPN，OpenAI/Anthropic 无凭据 HTTPS 请求均返回预期 401；移出后恢复原出口。全局模式返回真实公网 DNS 答复、同一 VPN 出口，两个 API 仍返回 401，局域网 IP 保持可达。
- 正常断开后 loopback DNS、TUN 路由和映射 IPv6 路由均清除，原网络 DNS 与 Tailscale 保留。恢复原名单代理及原七项名单。线路/订阅字节未变；公司配置内容完全相同，仅 JSON 键序因正常保存改变。额外公司认证已取消，未提交公司密码。
- 验收探针在系统临时目录的 Foundation 与 Go 路径规范化存在差异，换至项目外规范路径后完成 App 分流验收，未放宽路径身份校验；该差异不记为所有临时路径应用受支持。

## 隔离在线更新

独立 UpdateQA 实际从 build 71 识别、下载、验签并安装 build 72；PID 10906 → 12615，连续 12 个 0.2 秒样本为单实例。163 个普通文件与 9 个符号链接和目标 QA 包一致，签名通过。正式 ZIP 未为 QA 改写。

模拟连接和草稿开启时完成真实下载；草稿阻止最终安装，明确恢复安装后才结束模拟连接。新版可再次切换模拟连接；篡改整个签名 feed 后，实际客户端拒绝更新，ZIP GET 保持一次。QA 偏好恢复到原值。隔离期间正式 App、全部生产会话 PID 与三份配置哈希未变。

## 附件与公开回验

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| GoConnect-1.0.12-macos-arm64.zip | 32137508 | `7dd1d62ac74cf3f730fbc99a680afab22f2bdefcddef2e04af101737f3446597` |
| GoConnect-1.0.12-third-party-sources.tar.gz | 216709288 | `e2bd80daaefc99eccb983e74e1f1ab036e57df6e6ed550552ec7d6680d26c421` |
| appcast.xml | 1498 | `d8592fa8acf99fd7af5eb62bb624875f1b894ad5dc102c560e94bc0ab488399a` |
| SHA256SUMS | 287 | `cd8b85a94b31e29086cc6f4e8b9b57793182ae0afd2c93a5d2af64c16c52e63b` |

四附件 draft 元数据核对后公开为 [正式 Release](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.12)，Release ID 438665471。匿名核对 latest、tag commit、四个完整 digest；完整下载 ZIP/XML/SHA256SUMS 并逐字节比较，官方 Sparkle 工具回验 feed 和 ZIP 的 Ed25519 签名。

对应此 App 的第三方源码已审计 138 条来源、122 个 Go ZIP h1、198 个 tar 文件。公开源码包采用完整 GitHub digest 加匿名 128 KiB Range 比对，未重新完整下载源码包。

公开资产通过后才 fast-forward 推送 updates 提交 `3e4a2511b1e4493fe5f9b3f3bfcaeddcd8aad31e`。[固定源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml) 的匿名 curl 与原生 URLSession 最终均读回上表完全相同的 build 72 字节；不同 CDN 出口曾短暂读到旧 71，等待缓存刷新后，真实应用内检查也显示 1.0.12。没有添加 URL 查询参数或改客户端生产源。

本地保留 current 1.0.12 / 72、rollback 1.0.11 / 71，各一个 ZIP 与 SHA256SUMS；检查通过。后续 1.0.13 验收仍在进行，本轮共享临时材料在该流程结束后统一清理，验收记录保留。

## 发布后检查发现与边界

最终更新结果弹窗停留期间，记录到主线程确认信号中断约 35 秒，30 秒租约看门狗撤销 lease，服务按保护路径清理，主线已正常恢复。隔离原生模态 RunLoop 用例已复现此问题；1.0.13 将修正模态心跳调度，保留真正主线程卡死时的断开保护。此项不是 DNS 或公司 socket 回归通过的替代证据，也不覆盖已发布附件。

公司实网认证和真实副线再次连接本轮未执行。公司业务仍限明确 IPv4/TCP；独立 DNS 查询、App 自带 DoH、Finder 内核 SMB 及所有企业网络不作普遍保证。历史 AnyConnect UDP 按已接受的“部分缺陷已修复，剩余偶发超时已接受，发布非阻断”处理，未重新完成该稳定性验收。

原始证据：项目外私有目录 `~/.codex/visualizations/2026/11/09/goconnect-release-1.0.12/`；不公开凭据、公司地址或业务数据。
