# GoConnect 1.0.13 发布与验收记录（2026-11-09）

**发布完成：GitHub stable/latest、四附件、完整 feed 与 ZIP 签名及固定生产源均已公开回验。真实应用内检查已显示 1.0.13。本机安装同一份 build 73，正常升级服务，原名单代理主线保持连接；公司副连接关闭。**

本版承接 [1.0.12 的 DNS 与公司 socket 修复](PUBLISH_1.0.12_2026-11-09.md)，并修复最终原生检查发现的更新结果模态弹窗误触发租约撤销问题。1.0.12 已公开后未覆盖原附件或 tag，本次使用更高版本/build。

## 源码与信任链

- 源码提交 `53215d67d77e9bd52598de289585c230078053df`；annotated tag 对象 `7713cc677472a455a342106190286cfd3691e9e1`。
- 沿用团队 RX44JC39GB 的 Apple Development + Hardened Runtime、正式 Bundle ID 和原 Ed25519 钥匙串账户/公钥。不是 Developer ID 分发签名，未经过 Apple 公证；最低 macOS 26.0 / Apple Silicon。
- stable、固定 HTTPS feed、完整 feed 验签和 ZIP 解包前验签均开启。自动下载/安装关闭，自动检查默认关闭，已有用户的开启选择保留。未放宽 helper CDHash、文件身份或授权边界。
- 发布分支未带入另一开发分支尚未验收的地址导入功能。

## 原问题与适用回归

1. 1.0.12 最终检查中，原生更新结果弹窗使主线程确认信号停顿约 35 秒，既有 30 秒看门狗撤销租约并正常清理。隔离真实 RouteClient 在模态 RunLoop 保持 32 秒的测试先失败，修正后通过。
2. 心跳定时器增加原生 modalPanel 模式，并在主 RunLoop 内同步确认响应，避免等待该模式暂停处理的主 actor 排队任务。看门狗的 30 秒阈值、原 inode/UID 验证和永久卡死撤销逻辑不变；五项专项回归通过。
3. `--verify --team-sign` 退出 0，280.651 秒；39 项 Python、194 项 Swift、适用 Go race、真实打包组件/CSTP 与 DNS 核心检查通过。打包集成 128.209 秒；专用原生 TUN 自动测试的显式跳过不计为通过。
4. 冻结包内 Transport/OpenConnect 再完成四轮同路径公司 socket CSTP TCP 回归，其中两轮 SIGKILL 本轮 hook，停止所属进程组后均安全清理并可重新绑定。此为真实组件与隔离 TLS fixture 的回归，不是本轮公司服务器认证。

## 在线更新与已安装验收

- 隔离 UpdateQA 从 build 72 实际下载、验签、安装并重启到 73，PID 24443 → 24880；连续 12 个 0.2 秒样本为单实例。163 个普通文件和 9 个符号链接与目标包相同，签名通过；正式 ZIP 未为 QA 改写。
- 模拟连接与未保存草稿开启时真实完成下载；草稿阻止安装，明确恢复后才结束模拟连接并安装。篡改签名 feed 被实际客户端拒绝，ZIP GET 保持一次，新版未残留连接锁。QA 偏好已恢复原值。
- 同一授权验收窗口中正常断开、退出、备份和替换应用，再按诊断页正常升级服务并恢复原主线。172 个文件/符号链接与冻结包一致，helper 二进制 SHA-256 与同包 Transport 一致；三份配置哈希与补丁安装前一致。
- 真实生产 App 的原生更新结果模态弹窗在两个观测点间持续 85.039 秒，期间连续采样 55.43 秒、97 个样本：七个生产进程与原 lease 身份不变，记录 55 个不同续约时间，最大 lease 年龄 1.026 秒。六次实际代理请求均为 HTTP 200；关闭弹窗后仍已连接、速率继续更新，服务 ready/busy。
- 原名单代理下，公司域名 HTTPS、原局域网 HTTP 服务与国内网页只读请求均 200。公司功能保持关闭，没有再次要求公司认证；名单内/名单外 DNS 出口与全局模式的详细原生对照证据见 1.0.12 记录，本版未改动该 DNS 路径。
- [发布规范](RELEASING.md) 已补充已安装原生模态弹窗至少 45 秒、实际请求与租约身份的门禁，不能由模拟连接或无限续约替代。

## 冻结附件与公开读回

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.13-macos-arm64.zip` | 32137724 | `8fd93edf43551cbfb918b085b76a1025fc57b936239feefb6d86d2ca5eeefedb` |
| `GoConnect-1.0.13-third-party-sources.tar.gz` | 216709227 | `97dd4d7af32f6821ec3e4e8322b76d7f0aee794ca5a7c158013e8c596595c2ce` |
| `appcast.xml` | 1527 | `ab4b7d0c7c85c7346e3ef32b98bf1556d2fe14570e312e9b9e81d686e6f24ae1` |
| `SHA256SUMS` | 287 | `8a53a61f470817261fae38a755c610efbd09653f1db9962aa2f451d759ef0385` |

四附件先上传 draft，核对 uploaded 状态、完整 GitHub digest、尺寸及远端 tag commit 后，公开为 [1.0.13 正式 Release](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.13)，Release ID 438679487，stable/latest 正确。匿名完整下载 ZIP、XML、SHA256SUMS 并逐字节比对；官方 SDK 回验 XML 和 ZIP 的 Ed25519 签名及 enclosure 版本/长度/地址。

第三方源码对应冻结 App，本地审计 138 条来源、122 个 Go 模块 h1、198 个 tar 文件。公开回验采用 GitHub 全包 digest 与匿名 128 KiB Range 比对，未重新完整下载源码包。

公开资产回验通过后才 fast-forward 推送 updates，提交 `4f6df1ecdfb6b21408599d9b036c6558d2e5dbd1`，父提交 `3e4a2511b1e4493fe5f9b3f3bfcaeddcd8aad31e`。客户端使用的 [固定原 URL](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml) 经匿名 curl 和原生 URLSession 均首次读回上表完整相同的 1527 字节，build 73、签名和元数据正确；真实应用内检查也显示 1.0.13。没有 cache-busting 查询参数、force push、重建密钥或覆盖公开附件。

## 保留、收尾和边界

已保留 current 1.0.13 / 73、rollback 1.0.12 / 72，各一个 ZIP 与 SHA256SUMS，清单检查通过。安装前应用与对应回滚 ZIP 的文件一致；没有回滚用户配置。原始证据在项目外私有目录 `~/.codex/visualizations/2026/11/09/goconnect-release-1.0.13/`，1.0.12 原问题记录亦保留。本轮临时 App、测试服务器、签名 SDK、源码下载和候选副本在两包核对后清理，不触碰运行中的应用或网络组件。

公司副连接本轮未进行实网认证/重连；仅给出前述组件回归范围。公司功能仍限明确 IPv4/TCP。历史 AnyConnect UDP 按已接受的“部分缺陷已修复，剩余偶发超时已接受，发布非阻断”处理，未重新完成该稳定性验收；App 自带 DoH、独立 DNS 查询、Finder 内核 SMB 及所有企业网络不作普遍保证。
