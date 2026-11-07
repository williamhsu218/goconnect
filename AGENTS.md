# GoConnect 项目边界

- 源码、测试、打包资源、维护脚本和有效的技术记录保留在仓库中。
- 项目内只保留两个已验收的分发包：`dist/current/` 正式版及 `dist/rollback/` 紧邻的回滚版。每个目录仅含一个 ZIP 和 `SHA256SUMS`；版本与构建号记录在 `dist/releases.json`。
- 普通构建不替换正式版，也不自动滚动回滚版。默认入口为 `./script/build_and_run.sh --verify` 或 `--build-only`；Swift scratch、网络组件、图标和临时应用均在系统临时目录内生成，结束、失败或中断后自动清除。
- 不在 `.build/`、`Transport/bin/`、`dist/` 或其他项目目录留下候选包、预览应用、重复 ZIP、下载分片、测试日志或第三方研究克隆。直接使用 SwiftPM 时，显式指定项目外的 `--scratch-path`，结束后清理。
- 需要交付候选 ZIP 时，用 `--export` 导出到项目外的新目录；交付或验收结束后清理。GUI 预览仅用显式 `--run`，退出后清理临时应用。
- 只有得到对应安装或发布授权、完成适用验收后，才能用 `script/release_artifacts.py retain` 更新正式版和回滚版。版本号更新或构建成功本身不构成正式验收。
- 清理先执行 `python3 script/release_artifacts.py clean` 查看清单，再加 `--apply` 执行。仅处理项目内生成文件，跳过 `.git` 与受版本控制的文件，拒绝移动正在运行的产物；废弃产物移入废纸篓。
- 不因构建或清理退出 `/Applications/GoConnect.app`、中断 VPN、改写用户配置或删除特权服务的运行组件。安装包回滚和用户数据回滚是两个独立动作。

## 正式发布与应用内更新

- 后续正式版统一执行 [docs/RELEASING.md](docs/RELEASING.md)。发布请求的完成范围包含团队签名、GitHub Releases、签名更新源同步及公开回验；只上传 ZIP、创建 tag 或构建通过均不算发布完成。普通构建仍不产生安装或发布授权。
- 延续已验收的身份与信任链：团队 `RX44JC39GB`，现用 Apple Development 签名及 Hardened Runtime；GoConnect 独立 Ed25519 钥匙串账户 `com.willhsu.GoConnect`。发布必须显式 `--team-sign`，不允许退回 ad-hoc。证书续期须保留团队并验证升级兼容性；公钥、账户、bundle ID、固定 feed 或签名方式变更必须先做迁移方案和旧版升级验收，不得每版重建更新密钥。私钥、密码和令牌不进入源码、日志或 GitHub。
- 开始后续开发或发布时，先核对默认分支与最新正式 tag 的源码版本；默认分支落后时，以最新已验收 tag 为起点创建工作分支，避免基于旧源码构建。
- 版本以 `VERSION.json` 为源，Swift/Go 常量同步；正式版的语义版本和数字 build 均须高于现有正式发布与生产 feed。tag 必须指向验收源码提交，附件必须是该提交构建、验收并冻结的同一份字节；签名后不改包，公开后不覆盖 tag 或同名附件。
- 固定仓库 `williamhsu218/goconnect`，固定生产源 `https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml`。正式包须启用更新、使用 stable 渠道，保留 `SURequireSignedFeed` 与 `SUVerifyUpdateBeforeExtraction`；包内公钥与既有客户端兼容。预发布不得覆盖正式 feed。
- 严格按“完整检查与隔离真实 OTA → 四个附件上传为 draft 并核对 → 发布正式 Release 并匿名回验 → 提升已签名 feed 到 updates 分支 → 固定 URL 匿名回验”的顺序执行。附件为应用 ZIP、对应第三方源码包、`appcast.xml`、`SHA256SUMS`。整个 XML 和 ZIP 分别签名；XML 有任何改动都重新签名。feed 不得指向 draft、缺失、未验收或被替换的附件。
- 放行必须证明旧版可识别更高 build、签名/团队/公钥正确，隔离 App 真实完成下载、验签、安装和单实例重启，并验证活跃 VPN/草稿保护与篡改拒绝。公开回验须核对 tag 提交、stable/latest、附件尺寸与 SHA-256、匿名下载、固定 feed 与已验收 XML 逐字节一致、feed/ZIP 的 Ed25519 签名和 enclosure 元数据。不能把 HTTP 200、上传成功或模拟状态当成完整验收。
- 缺少更新源同步或最终回验时，明确报告“发布未完成／更新源未放行”，修复后继续完成，不宣称应用内更新已可用。推送失败先检查并发发布，禁止 force push 覆盖 updates；生产 feed 不回退到更低 build。回滚策略见发布规范。
- 保持自动检查默认关闭且可由用户开启，下载和安装由用户确认；保留现有更新互斥、退出前复核及 helper 精确 CDHash 边界。网络组件变化按诊断页正常升级服务，不静默中断 VPN，不自动回滚数据。
- AnyConnect UDP 历史问题按用户 2026-11-07 确认标记为“部分缺陷已修复，剩余偶发超时已接受，发布非阻断”：不阻断不依赖该能力的 HY2、UI、更新链路及 IPv4/TCP 公司副连接发布，不默认继续安排该项长测。尚未重新完成 UDP 实链路稳定性验收，不得记为全部修复或 UDP 验收通过；未来修改该能力或业务重新依赖 UDP 时，另定适用验收。其他发布门禁不变。
- 将版本、源码/feed 提交、资产哈希、安装/OTA/保护/公开回验证据及已知限制记录到 `docs/PUBLISH_<版本>_<日期>.md`，原始证据放项目外私有目录。完成适用验收后才 retain；只保留规定的正式/回滚两包并清除本次临时产物。
