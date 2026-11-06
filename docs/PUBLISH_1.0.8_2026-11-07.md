# GoConnect 1.0.8 / build 68 发布验收

2026-11-07：系统 DNS 接管通过隔离原生验收，正式 Release 与签名更新源已发布并公开回验。本轮未替换 `/Applications/GoConnect.app`，未中断当前 VPN；实际生产安装仍为 1.0.7 / build 67。用户更新 App 后须按诊断页正常流程更新网络服务，后续重新连接时启用新 DNS 路径。

## 发布身份与来源

| 项目 | 已核验值 |
| --- | --- |
| Release | [v1.0.8](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.8)，ID `436741592`，公开 stable/latest |
| 验收源码 / tag 指向 | `2b5e4f1140fad3d1a0321bc6d41faf241dcc40c4` |
| 基线源码 | `323a5051f9fc0a67049da25724aea145d769f3d9`，已验收 1.0.7 主线 |
| 更新源提交 | `bee53ca14fb6b4a28b09dee27e74ae132361b946`，正常 fast-forward 提升 `updates` |
| 固定 feed | <https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml> |
| 团队 / 签名 | `RX44JC39GB`，Apple Development，Hardened Runtime |
| 更新账户 / 公钥 | `com.willhsu.GoConnect` / `yoZPyYSmkseQ5kBGctYNGSuVCaNP/59l23vdNqJxA70=`，与旧版相同 |
| 包配置 | 生产 Bundle ID、stable、固定 URL、强制签名 feed、解压前验签；自动检查默认关闭 |
| 系统要求 | Apple Silicon / macOS 26 及以上 |

验收源码、构建、签名冻结和发布附件一致。此记录在发布后以文档提交补充，不移动 `v1.0.8` 或替换任何公开附件。原工作区既有修改和索引保留，源码构建在独立 worktree 完成。

## 四个冻结附件

| 附件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.8-macos-arm64.zip` | 31774145 | `46137d29a1957958f58a9130e26fa31bc1ed2e0f739e2d396cda7ec148692f4e` |
| `GoConnect-1.0.8-third-party-sources.tar.gz` | 216708943 | `f2a0bc0b9cd1993f22092c5e4b25cd4a72ce8c6643ee02c5d980876c013662a7` |
| `appcast.xml` | 1563 | `8c2442e7cb784a7776b5f9f70e72b68ebeccb1fd8e54232d224271d50b0d7bae` |
| `SHA256SUMS` | 285 | `2f6b82c54d035d53a6f1091eef4a845034c3e1e99b22a993b7e22d359371b5cf` |

draft 阶段确认恰有四附件，尺寸、GitHub 完整 digest、tag 解析提交均匹配。公开后匿名完整下载应用 ZIP、XML、校验清单，与冻结文件逐字节/哈希一致；ZIP 和完整 XML 的 Ed25519 验签通过。第三方源码完成本地全包、来源及模块审计；公开回验使用 GitHub 完整 digest 加匿名 64 KiB Range 对比，**没有重新完整下载第三方源码包**。

Release 元数据由已认证 GitHub API 经 CLI 读取；latest 页面跳转、附件与固定 feed 请求均不携带 GitHub 登录凭据。匿名 `/releases/latest` 指向 v1.0.8。feed 推送前核验旧 build 67，无并发更高版本；生产源未回退。

固定原 URL 最初命中旧 CDN 缓存；有界重试第 6 次取得 build 68，与发布 XML 完全一致并验签通过。macOS 原生 URLSession 也直接读取固定 URL 得到相同 1563 字节、相同哈希和有效签名；没有用随机查询参数或 commit URL 代替验收。

## 功能与构建证据

执行 `GOCONNECT_SIGN_TEAM=RX44JC39GB ./script/build_and_run.sh --verify --team-sign --export <项目外临时目录>`，退出码 0：39 个 Python 测试、142 个 Swift 测试、Go race 全包检查、打包内核与传输集成检查通过。逐层严格签名及正式包更新配置检查通过。常规套件中需特权安装环境的既有用例没有冒充本轮通过；DNS 的原生权限路径另行执行如下验收。

- 使用最终源码及最终包内 Mihomo 验证公网/AI 查询经认证 SOCKS 进入 DoH，显式国内直连规则及私有解析保留；精确域名、子域名、单标签、接口/端口及 IPv6 link-local zone 有适用检查。
- 代理 DoH 人为失败时受保护查询返回失败，不向原运营商 DNS fallback；国内直连查询仍可用。
- 原生随机测试域名通过 macOS 查询进入候选监听；正常退出及 worker SIGKILL 后，独立临时 DNS 记录自动撤销。
- 实链路验收沿用正在运行的节点代理，不建立第二个 TUN、不更改系统路由、不安装候选 helper。短时临时默认 DNS 接管下，OpenAI、Anthropic、Gemini 与百度、淘宝、QQ 查询成功。
- 权威 DNS 探针在原系统路径观察到运营商解析器，在候选路径观察到 Cloudflare/Google 解析器。单个新域名故障注入下，macOS 未重试原 DNS，权威探针没有收到直连解析。该结论只覆盖此原生解析路径与测试窗口。
- 局域网路由器 HTTP 在接管前、期间、结束后均为 200；系统 DNS 完整恢复。7 个正式 App/网络进程与 39 个配置 JSON 的哈希保持不变，原工作区索引保持不变。

实现、失败恢复和适用边界见 [系统 DNS 说明](SYSTEM_DNS_2026-11-07.md)。本次未重复创建生产 TUN 或替换运行服务，以保留用户现有 VPN；连接生命周期与新增 DNS 失败分支由套件覆盖，原生 DNS 本身由上述真实授权测试覆盖。

## 隔离真实 OTA

使用独立 `com.willhsu.GoConnect.UpdateQA`、临时用户数据、同团队和同更新公钥，从已验收 build 67 更新至 68；生产安装包保持冻结。

- 真实识别、下载、验签、安装及自动重启完成。复测旧 PID `67456` 变为 `67598`，交接期间连续 12 个只读进程样本均只有一个新主进程；163 个普通文件与目标包一致，严格签名有效，实际窗口显示 1.0.8。
- 模拟 VPN 连接时可检查、下载和验签；未保存草稿阻止最终安装。新版本无更新检查、失败和取消后均可再次连接，不残留更新锁。本轮没有为 OTA 模拟状态测试断开真实生产 VPN。
- 修改 feed 的 build 而不重新签名，客户端明确拒绝；没有新增更新 ZIP 下载，取消后连接门禁释放。
- 首次尝试在重启期间调用 UI 工具后观察到两个 QA 进程，该次单实例项未放行。重新从原始 build 67 验收，重启交接仅使用只读进程观察，确认自动启动单实例后再检查 UI，才记录通过。未将首次异常原因未经证实地归结于 UI 工具。

测试 App 与本地 QA feed 服务已退出。没有对正式 App 执行安装、连接或断开。

## 保留与证据

`release_artifacts.py retain` 及 `check` 通过：`dist/current/` 为 1.0.8 / 68，`dist/rollback/` 为 1.0.7 / 67（ZIP SHA-256 `5405459fc4eb11c9e22162d3cc246a3afd98fc9faf7d01f59e80ac6ff8435114`）。每个目录仅一个 ZIP 和校验清单；项目清理预览为空。用户数据不随安装包滚动回滚。

原始证据保留在本机项目外私有目录 `~/.codex/visualizations/2026/11/07/goconnect-system-dns/`，包括：

- `build.log`、`validation.json`、`candidate-package.json`、`native-final.log`；
- `lan-preservation.json`、`production-preservation.json`；
- `qa-first-attempt.json`、`qa-acceptance.json`；
- `public-release-readback.json`、`public-feed-readback.json`、`native-feed-verified.json`；
- `retained-releases-check.json`、`project-cleanup-preview.json`，以及本次临时目录收尾回执。

临时 worktree、候选 App、测试二进制、下载及第三方源码仅位于本次系统临时目录，收尾移除；保留源码提交、公开资产、项目规定的两份分发包和私有验收证据。

## 已知边界

现有 Apple Development 签名不是 Developer ID，未经过 Apple 公证；不能据本机 OTA 推断其他 Mac 首次安装的系统信任行为。AnyConnect UDP 历史限制未修复、未重新验收。

本次保护系统默认 DNS。用户明确的直连 DNS 例外、应用自带 DoH/DoT、指定接口查询及 WebRTC 不等同于该保护范围；应用流量仍遵守已有分流规则，不宣称全部应用零泄漏。不新增企业 VPN 内部 DNS 下发能力，也不提供断开 VPN 后的全局封网。
