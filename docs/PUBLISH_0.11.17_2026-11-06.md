# GoConnect 0.11.17 / build 57 安装与发布记录

日期：2026-11-06。用户已明确授权替换本机安装本并发布 GitHub Releases，随后要求替换时不调用 Claude。

## 变更与协作

采用已验收的浅色精密 UI 和本机按线路流量统计；本轮将“应用白名单”改为“代理应用与服务”，模式显示为“按名单代理”。内部 whitelist/global 标识不变。相对公开 v0.11.16，Transport 目录仅更新版本常量，订阅加载/保存和协议实现未更改。

Codex 执行命名、集成、构建、源码脱敏、实际安装验收和发布。Agy 请求固定为 gemini-3.8-flash-high / high，完成限定文案 diff 复核；平台仅提供会话元数据和自报，缺少独立的模型与强度回执。Codex 独立子 Agent 审查公开快照、服务更新和第三方源码附件。本轮未调用 Claude，也未触发模型限额。

## 构建与源码

- 完整 --verify --export 通过：Swift 112 项，Python 28 项，Go race、严格签名、运行组件和隔离集成检查。需真实系统分流的条件测试按环境跳过。
- 候选和安装 App 的76份文件逐文件 SHA256 完全一致，配套 helper Runtime 与安装包 Runtime 逐文件一致。
- 公开快照在项目外独立 worktree 准备，产品 Sources 与构建 Sources 逐文件一致。
- 三份含私人测试地址的夹具恢复为 v0.11.16 公开版本，脱敏后相关 Python 9 项和 Swift 7 项测试通过；远端 workflow 树保持原状。
- 本版仍为预发布，AnyConnect 真实 UDP 长期稳定性门禁未通过；不滚动 dist/current、dist/rollback。

## 实际安装与界面

先通过旧版正常断开，核对旧服务 ready/busy=false、网络子进程退出，再通过菜单退出旧 App，确认没有从安装包运行的进程后换包。旧包保留在项目外，完整 ZIP 回滚备份另存。

安装 /Applications/GoConnect.app 0.11.17 / 57 后，通过诊断页正常更新服务，随后恢复原订阅线路与按名单代理模式。服务 ready/busy=true，helper 与应用 Runtime 一致，真实认证回环 SOCKS 请求取得 HTTP 204。已看到真实上下行计数增加并写入 traffic-history.sqlite，quick_check=ok。

安装本的连接页、名单页及分时、日、月、年统计页面均通过实际原生渲染和 AX 核对。保持当前原线路连接，没有进行额外企业 VPN 稳定性测试。

初次安装后检查中，configuration.json、subscriptions.json、旧用量缓存和同步端所有者文件均与基线完全一致。用户随后自行删除一条保存线路并将订阅3组调整为1组，已明确确认该操作；剩余线路内容、其他配置及旧凭据保持原样，不恢复备份覆盖用户的新状态。

## 本机证据与回滚

私有证据目录：/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-release-092346

- installed-readback.json：安装、helper、数据和SQLite回读。
- installed-final.jpg、installed-proxy-list.jpg、installed-traffic-*.jpg：真实安装界面。
- verification-build.log、public-fixture-tests.log：完整构建及脱敏夹具检查。
- rollback-GoConnect-0.11.16-build56.zip 与 rollback-SHA256SUMS：原安装包备份。
- data-backup/：安装前数据备份，权限0700；仅用于有针对性的恢复，不覆盖用户后续调整。

应用、helper 与用户数据是不同回滚对象。旧 App 回滚时须通过其诊断入口正常授权匹配的旧 Runtime，不将换回 App ZIP 误写为完整服务或数据回滚。

## 第三方附件核验

复用0.11.16第三方源码内容，保留内部旧版本目录名。全量读取211个tar成员、核对137条源码/模块记录SHA256，122个模块ZIP重算h1并与候选实际链接模块集合一致；SBOM、许可证和精确版本配方通过。旧安装版和候选ThirdParty的54份文件中仅README多7行发布说明，其余53份相同；tar中的说明与候选一致，因此该文档差异不阻塞复用。完整结论见 reused-sources-validation.json。

## 公开发布与回验

已公开为 [v0.11.17 预发布版](https://github.com/williamhsu218/goconnect/releases/tag/v0.11.17)，published_at=2026-11-06T01:48:04Z，draft=false、prerelease=true。分支 codex/release-0.11.17 与标签对应源码提交 daad707a929265a9b17ba8c683b3f05db0415fa2。main 和最新稳定版 v0.11.10 未更新。

三个附件均上传完毕并核对服务端SHA256。匿名完整下载30,647,210字节的应用ZIP及完整SHA256SUMS，与本机候选完全匹配；匿名API读取确认发布说明、预发布状态、文件大小和三个附件摘要。源码附件在上传前完成全量校验，上传后检查服务端完整摘要与匿名64KiB范围下载，未再完整重复下载199,919,705字节源码附件。证据为 public-download-readback.json。

应用ZIP SHA256：2c51ee1a2818a7438b9046c65826c21568a31a9d73924a8eaf9cc34be854dcbf。
源码附件 SHA256：93ab565bfee23aefc2ea542dbda25440aed2e4f9cafd465afd51273830e1ed43。

本轮临时 worktree、候选包、下载分片和测试 scratch 已在交付结束清理。保留原安装包回滚ZIP和私有证据；不滚动项目正式包与回滚包，不覆盖用户在验收期间主动修改的数据。
