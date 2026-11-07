# GoConnect 1.0.9 发布与更新源回验（2026-11-07）

GitHub 正式 Release、签名生产 feed 和当前安装客户端的更新提示均已回验。维护者本轮明确选择“先发布，由本机在线更新后做真实副 VPN 场景验收”；公司副连接以**试用、默认关闭**交付。该选择仅调整本版副连接真实场景的验收顺序，不把未执行项目记为通过，也不改变后续版本的一般发布门槛。

本轮未替换 `/Applications/GoConnect.app`、未升级当前生产 helper、未中断 VPN。结束回读仍是 **1.0.8 / 68**，网络服务 `ready / busy`；当前客户端已显示 1.0.9 可下载，留给用户确认下载和安装。

## 身份与版本

| 项目 | 结果 |
| --- | --- |
| 版本 / build | 1.0.9 / 69 |
| 构建 / tag 源码 | `29f6289a5870870da8fa19d7fdea47c25953f48a` |
| annotated tag 对象 | `6e856b4494a4910b8f9a015770887ac7d8925f22` |
| feed 提交 | `d7e59bd703c893a938aa7173b398e7bdddd3b29f` |
| Release | https://github.com/williamhsu218/goconnect/releases/tag/v1.0.9 |
| 生产源 | https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml |
| App / 服务身份 | `com.willhsu.GoConnect` / `com.willhsu.GoConnect.network`，包内 helper 回报 production |
| 签名 | 团队 RX44JC39GB，现有 Apple Development，Hardened Runtime |
| 更新 | stable，原 Ed25519 公钥与钥匙串账户不变，强制 feed 和解包前 ZIP 验签 |
| 系统 | Apple Silicon，实际最低 macOS 26.0 |

默认自动检查关闭；现有用户主动开启的偏好保留。下载和安装仍由用户确认。现用开发签名不是 Developer ID 分发签名，未经过 Apple 公证。本机验收不代表其他 Mac 首次安装的信任行为通过。

## 冻结附件

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.9-macos-arm64.zip` | 32112773 | `8024ffb306b8121805abe0844dd673df9d1bf96c0ac46fdbbe4eaace5d1c7cf4` |
| `GoConnect-1.0.9-third-party-sources.tar.gz` | 216709000 | `f234d7b46ac5133dcfb7e5c6611f8927d2f7ca926bab6755b2875b3f7f67b323` |
| `appcast.xml` | 1583 | `75f6c052281067b42d68bfba78a9c4fdf692f086b71f3bb7cc728f6e8a3aa1e2` |
| `SHA256SUMS` | 285 | `f1e4df734f2611862d69f7e24c54782cff467ea764a6f3fc3e51e7179992aa77` |

四附件先上传 draft 并核对 uploaded 状态、长度和 GitHub digest；tag 解析至上述源码提交后才公开为 stable/latest。附件公开后没有覆盖 tag 或同名文件。主仓库及其 README 另同步版本与本文记录，tag 保持指向实际构建提交。

## 本地与 OTA 验收

- 正式构建执行 `./script/build_and_run.sh --verify --team-sign --export <项目外临时目录>`。39 项 Python、190 项 Swift、Go race 全包、实际打包组件和隔离集成检查通过；逐层严格团队签名与 production / stable 配置通过。首次 production 构建暴露 macOS Bash 严格模式展开空参数数组的问题，已在构建提交中修正并重跑完整构建，失败产物自动清除。
- 没有公司 sidecar 时，配置缺省 disabled，原主线路不创建公司 Gate、额外 DNS 租约或网关路由。副连接草稿、状态机、实际传输和根控制器进入更新保护；独立 Codex 子 Agent 只读复核未发现本次范围内的阻断级缺陷。
- 在独立 `com.willhsu.GoConnect.UpdateQA` 中从保留包 build 68 真正更新至 69；测试包分别重新团队签名，QA feed 与 ZIP 分别签名，主程序文本与原包一致，生产 ZIP 不修改。
- 旧版识别新版本、真实下载和验签完成；模拟 VPN 连接中可下载，草稿未保存时阻止最终安装，草稿与模拟连接状态保留。
- 解除草稿并明确确认后正常停止模拟连接、安装和自动重启：PID `17020` → `18946`，连续 12 个 0.2 秒间隔样本只有一个新主进程；163 个普通文件与目标 QA 包相同，严格签名通过。交接期间不调用会启动 App 的 UI 入口。
- 新 build 无残留连接锁；无更新、失败取消、开启自动检查后冷启动均可再次切换模拟连接。篡改 QA feed 后，实际 Sparkle 显示验签失败；ZIP 请求数仍为 1，没有下载被篡改更新。正式 App 和真实 VPN 没有参与模拟断开。
- 最终包依赖和源码绑定已验证：按实际包收集 138 条组件 / Go 模块来源，所有文件 SHA-256、适用 h1 校验与来源检查通过。

Go 工具链未在此工作目录为 helper 输出 VCS 字段；构建前后 clean 源码提交、冻结 ZIP、脚本与验收回执在项目外记录，不虚构包内 VCS 证明。

## 公开与实际客户端回验

- 匿名 API 确认 `draft=false`、`prerelease=false`、latest 为 v1.0.9，tag commit 与四附件元数据匹配。
- 匿名完整下载应用 ZIP、appcast.xml、SHA256SUMS，长度和完整 SHA-256 与冻结字节相同；官方 SDK 分别验证完整 XML 及 ZIP 的 Ed25519 签名。第三方源码包采用 GitHub 全包 digest 加匿名 128 KiB Range 与本地同段比较，**未重新完整下载全包**。
- 在资产公开回验之后，才把已签名 XML 原样 fast-forward 推送到 updates；未 force push。固定 URL 的匿名 curl 回验及原生 URLSession 回验均得到与冻结 XML 相同的 `75f6c052281067b42d68bfba78a9c4fdf692f086b71f3bb7cc728f6e8a3aa1e2`，build 69 / enclosure 元数据和签名正确。
- 原生请求首次拿到旧 build 68，即使匿名 curl 已返回 69；未更改正式 App 的固定 URL、缓存策略或签名要求。继续回验后原生请求读回正确 69，当前运行的正式 1.0.8 才显示 1.0.9 更新提示。不能用最初的 HTTP 200 代替客户端通过。
- 保留包已滚动为 current 1.0.9 / 69、rollback 1.0.8 / 68；各目录仅应用 ZIP 与 SHA256SUMS。安装包回滚与用户数据回滚独立；不回滚或覆盖用户数据。

## 真实副 VPN 仍待验收

系统 TUN / IPv4 与 IPv6 路由、原生公司 DNS、公司真实只读业务 / SMB、主副连接故障隔离、睡眠恢复及退出清理，仍须在用户更新后、按单独窗口实际验证。公司业务本阶段只支持 IPv4 / TCP。AnyConnect UDP 历史限制未修复、未重新验收，不把隔离 CSTP 或 HY2 成功记成真实 UDP 通过。

更新 App 后先按诊断页升级网络服务，保持公司副连接关闭验证原主线路，再于主连接断开时配置公司线路、批准范围和域名。DNS 可留空，连接公司线路后获取。详见 [发布说明](RELEASE_1.0.9.md) 和 [功能测试版验收记录](DUAL_CONNECTION_INTEGRATED_TEST_2026-11-07.md)。

## 证据与收尾

原始证据位于本机私有目录 `~/.codex/visualizations/2026/11/07/goconnect-release-1.0.9/`，包括构建、候选与四附件清单、QA 阻断与篡改截图、安装文件比对 / 单实例采样、公开下载、固定 feed、原生请求、当前客户端更新提示及生产保护回读。企业地址、订阅、凭据和原始网络日志不进入 GitHub。

候选 / SDK / 第三方源码 / 公开下载和 QA 数据仅在系统临时目录；收尾停止本次 QA server、移除临时 App 和工作副本。主项目普通清理清单及正式 / 回滚 pair 回验结果另存私有回执。保留用户正在运行的正式 App、网络组件、当前配置与 VPN。
