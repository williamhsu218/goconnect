# GoConnect 1.0.3 更新状态修复与发布验收

日期：2026-11-06。版本 1.0.3，build 60，stable。

- [正式 Release](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.3)，ID `436123805`，非 draft、非 prerelease，已为 latest。
- 源码/tag：`87c9c9685841673dc7eeba78d24ea2c40d49d2c0` / `v1.0.3`。
- 默认 main 合并：`1c3ea43f6e96cdd8316c6ee2196d0287f7de24d0`，保留原 main 与发布源码为父提交；树与验收源码完全相同：`af8ba7bcbbcf97293e9000a24e95819a94fbbd7d`。
- 固定签名 feed：https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml
- updates 提交：`e92f0626bfe8e62b124773ecba7b53bcf9fab516`，从 `2ba1e71f3a3863be92ce08fcd8e96754aaa9adef` 正常 fast-forward，未 force push。

## 故障与变更

用户实际升级至 1.0.2 后出现“请先结束应用更新，再连接或维护网络服务”。正式 App 开启了自动检查。正常退出并重新打开仍复现；配置、应用字节和服务未改变。

根因是 Sparkle 在安排下一次自动检查时也会短暂切换 `sessionInProgress`，随后恢复空闲，但该过程没有对应的更新 driver 结束回调。旧观察器只处理 true，AppDelegate 又将整个会话作为连接门禁，导致冷启动或更新后的状态残留。实际 1.0.2 副本在隔离 QA 中开启自动检查可稳定复现。

本版同步处理两个状态边沿，并把检查/下载会话与安装交接分开。检查、下载和验签允许保持 VPN；下载选择及 relaunch 策略查询不设置安装锁。用户确认安装重启后，才调用 AppStore 原有断开路径，等待连接及网络服务实际停止，再交接已经验证的本地安装包。未保存草稿、节点测试、订阅操作及服务维护只暂停最终安装。失败或取消释放连接门禁；残留 installer 标记仅保留退出防护，不永久锁住联网。

另修复 Sparkle 的“无更新”结果被误报为检查失败。未修改 VPN 协议、路由核心或 helper 信任规则；Transport 相对 v1.0.2 仅生成版本常量变化。更新流量仍遵守用户分流规则，按名单代理时 GoConnect 需在代理名单中才会通过线路，不偷偷改写代理/直连配置。

## 验证

- 最终完整构建通过：141 项 Swift 测试、39 项 Python 测试、Go race、适用隔离网络集成、动态依赖和严格嵌套签名。生产特权服务/TUN 检查未执行；AnyConnect 真实 UDP 仍按用户决定跳过，未修复或重新验收。
- 使用实际已安装 1.0.2 的代码副本，真实升级至最终 1.0.3 QA 包，完成下载、验签、安装及 Sparkle 重启。新 PID 为 57045，旧 PID 为 56946，单主进程，文件匹配签名目标，严格签名通过。
- 连接中下载流程以新版代码配合 QA 降低 build 的元数据验证：VPN 模拟状态在检查、下载及验签期间保持；有草稿时最终安装暂停且不先断开；清除草稿并明确重试后正常断开、安装和重启。该轮完成后只修改了“已是最新版”结果文案及对应测试；最终包重新完成完整构建、旧版真实升级及连接状态下“已是最新版”回归。
- 自动检查开启的最终新进程可以连接；无更新后连接保持可用，显示正确文案。篡改签名 feed 被实际客户端拒绝，HTTP 记录无 ZIP 下载，取消后入口恢复。异步停止被取消时不调用过期安装 continuation 的测试通过。
- 旧版临时恢复方法也在实际 1.0.2 副本中验证：关闭自动检查后正常冷启动，连接控制恢复可用。未将此偏好改动应用到正式 App。
- 第三方源码对最终 App 审计通过：138 条来源 SHA-256、122 个实际链接 Go 模块 h1、15 个来源归档与 13 份配方/SBOM、Sparkle 源码和许可证、45 项组件元数据检查。
- 四附件尺寸、GitHub 完整 digest 与冻结清单一致。匿名完整下载应用 ZIP、feed 和 SHA256SUMS 并验签；第三方源码采用本地全包审计、GitHub 全包 digest 和匿名前 64 KiB 比较，未重复完整下载 216 MB。
- 匿名 GitHub API 触发 403 限流后，元数据由已授权 connector 核对；公开 latest 页面匿名跳转至 v1.0.3，页面与附件仍不带 GitHub 凭据验证。固定 feed 普通匿名请求和无 Cookie/凭据/本地缓存的原生 Foundation 请求均为 HTTP 200、1.0.3/build 60，字节一致且官方 Sparkle 验签通过。
- 正式 GoConnect 的真实 Sparkle 窗口已从固定生产地址识别“1.0.3 可供下载，当前 1.0.2”，停在更新提示，安装留给用户。

## 签名、保留与当前安装

沿用 RX44JC39GB 团队 Apple Development 签名及 Hardened Runtime；ZIP 和整个 feed 使用既有 GoConnect Ed25519 密钥签名。公钥与 feed URL 保持旧客户端兼容。未采用 Developer ID，未做 Apple 公证。

| 附件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| GoConnect-1.0.3-macos-arm64.zip | 31689517 | `bb2c64b5d50549c2e1dc19c395ffa35eac9dedf0362a0e6ae4ac24d69b41d56e` |
| GoConnect-1.0.3-third-party-sources.tar.gz | 216709026 | `3c8995e23482c912a2f755fdbf8e7920dfc8221bc1e3b551cf31d88ea63070b1` |
| appcast.xml | 1490 | `06a34adf82250dbe174f68aaba36d450d4239343c35d83e67d5ace93a7eb343a` |
| SHA256SUMS | 285 | `258a886aa257d364ce264450ff00540c15d216fa680aad76380f464188b86754` |

交付读回时，正式 App 仍为 1.0.2/build 59，应用文件及四份受保护配置哈希不变。初始排查仅正常退出/重开未连接的正式 App并尝试原线路，旧锁拒绝连接；没有替换 App、断开活跃 VPN、改写正式自动检查偏好或升级 helper。

1.0.1／1.0.2 的检查规则属于旧程序，本次迁移仍按旧限制；1.0.3 起可以连接中检查下载，确认安装后才断开。已下载包的安装交接不需要再次访问 GitHub。网络服务按原流程要求升级时，由用户正常完成本机授权。

`dist/current/` 保留 1.0.3/build 60；`dist/rollback/` 保留 1.0.2/build 59；每处仅一个 ZIP 和 SHA256SUMS，索引见 `dist/releases.json`。应用与用户数据回滚独立处理。原工作区未提交改动与 Git 索引保留，发布使用独立 worktree；只清除本轮临时目录、测试进程和 QA 偏好。

## 证据

私有证据目录：`/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-1.0.3-125747/`。

主要回执：`root-cause.txt`、`source-freeze.json`、`full-build.log`、`candidate-package.json`、`qa-ota-readback.json`、`qa-connected-ota-initial.json`、`qa-tamper-readback.json`、`third-party-validation-1.0.3.json`、`public-release-readback.json`、`public-feed-readback.json`、`native-feed-verified.json`、`main-readback.json`、`delivery-final-readback.json` 和 `cleanup.json`。实际界面包括 `production-update-offer.png`、`qa-vpn-during-download.png`、`qa-draft-deferred-vpn-preserved.png`、`qa-final-connected-current.png` 与 `old-workaround-auto-off.png`。

本轮实现、审核、构建、验收和发布由 Codex 完成，未调用 Claude、Agy 或子 agent。QA 的连接状态为隔离模拟，不冒充真实线路长时间运行测试。
