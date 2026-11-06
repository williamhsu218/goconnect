# GoConnect 0.11.8（build 45）修复与本机安装验收

日期：2026-10-12。已获用户授权，完成本机应用与特权服务升级，并恢复原线路连接。未发布 GitHub Release。

## 提示布局

- 成功提示直接渲染文本，图标、文字和关闭按钮垂直居中，文字区域左对齐。
- 错误提示按实际文字高度排版；超过 120 pt 才显示滚动区域，复制和关闭功能保留。
- 隔离原生预览检查了成功提示、两行断线提示、长文本滚动和关闭；预览使用临时配置与日志目录，无真实 VPN 会话。

## 意外断线提示及持久日志

- 补齐隧道直接退出（包括退出码 0）时的错误提示，测试隧道、本机分流及代理入口的意外结束均有提示入口。正常主动断开和迟延退出事件不会误报。
- 已建立会话发生异常时，显示持续错误提示与诊断导出指引；后台运行时请求 Dock 提醒，不自动抢占前台。
- OpenConnect 的 stdout 和 stderr 均通过固定分类器，只保留事件代码与数值。记录服务器断开、空闲超时、会话到期、凭据被拒、DPD 超时、TLS 失败、重连事件，以及协商的 DPD/keepalive/超时秒数。
- 记录实际 OpenConnect PID、退出码、退出信号、输出读取不完整、连接超时；记录主动断开、意外断线原因、组件来源和连接持续秒数。
- 等待输出读取完成再报告退出，避免最后的原因丢失；清理期间仍接收诊断事件。读取有超时边界，重复分类记录有界。
- 日志沿用 `~/Library/Logs/GoConnect/app.jsonl` 和 `app.jsonl.1`、0600 权限及原有轮转；「诊断 → 导出」包含新增事件与数值解释。绝不保存任意原始服务器文本、Cookie、密码、账号或请求正文。
- 订阅/Mihomo 的退出目前可以识别组件、退出码及信号；OpenConnect 的细分原因仅适用于 AnyConnect 会话。原因未取得时明确显示未知，不推定为服务器踢线。
- 本次保留既有重连策略，没有增加自动重连。网络和睡眠事件用于关联时间，不单独作为断线根因证据。

## 2026-10-11 15:00 事件核对

现有本机持久日志（`app.jsonl.1`，应用 PID 1146）确认：

| 北京时间 | 事件 | 数值 |
|---|---|---|
| 15:00:58 | transport.error / phase.stopping | 0 |
| 15:00:58 | transport.exited | 1 |
| 15:00:58 | phase.failed | 0 |
| 15:01:40 | transport.started | 27581 |
| 15:01:54 | phase.connected | 0 |

检查的 14:00–17:00 时间窗内，断线附近没有 `system.sleep/wake` 或 `network.unsatisfied` 事件。已有日志只能确认会话异常结束，不能区分服务器断开、TLS、DPD 或会话到期；新增日志不能补回历史缺失信息。

## 验证与交付边界

- 默认入口：`./script/build_and_run.sh --verify --export /tmp/goconnect-0.11.8-candidate-20261012`。
- Swift 60 项测试通过，包含直接退出提示、原因保留、主动断开不误报，以及 TransportClient 清理期间最终诊断落盘/拒绝未知代码。
- Go 分类、隐私及全量竞态测试通过；打包运行时及本地 CSTP/TCP/UDP、证书拒绝、服务器 Idle Timeout 断线验证通过，候选包签名和 SHA-256 校验通过。
- `/Applications/GoConnect.app` 为 0.11.8 / build 45，全部文件与已验证包一致，严格签名校验通过；特权服务的 17 个运行组件与应用包一致，服务状态为 ready。
- 用户在本机完成管理员认证及 VPN 密码输入，原线路重连成功。正式应用显示会话活跃、收发流量增长、透明 App 分流和 TUN 接管正常；10 个用户配置文件升级前后哈希一致。
- 在正式应用执行「诊断 → 导出摘要」，验证 0600 文件、断线原因数值说明、实际 OpenConnect PID / DPD 参数和可用服务日志。导出成功提示的图标、文字及关闭按钮在实际窗口中居中对齐。
- 意外断线的持续提示、原因分类和最终日志以隔离原生界面及实际打包 OpenConnect 的本地 Idle Timeout 场景验证。未对已恢复的公司 VPN 注入断线；真实网关未来发生异常时的具体原因仍需以当次日志分析。未新增睡眠唤醒验收。
- 本机分发保留 0.11.8 / build 45 正式包及与升级前安装版全部 76 个文件一致的 0.11.7 / build 44 回滚包。

本次证据目录：`/Users/willhsu/.codex/visualizations/2026/10/12/01a12d99-0334-7af0-86de-293f57e5e0b8/`，包含隔离预览 `goconnect-toast-success.png`、`goconnect-disconnect.png`、`goconnect-toast-long.png` 和合成断线日志；正式安装版 `goconnect-installed-connected.png`、`goconnect-installed-toast.png`、`GoConnect-diagnostics.txt` 及安装读回 `candidate.json`。

正式 ZIP：`dist/current/GoConnect-0.11.8-macos-arm64.zip`；SHA-256：`2640028a754e2fa04d874b147ba680e3f5694558fe6c9865192176052ed7a6fd`。回滚 ZIP：`dist/rollback/GoConnect-0.11.7-macos-arm64.zip`。完整验证日志保存在上述证据目录的 `goconnect-verify.log`。临时预览、scratch、候选导出和安装暂存目录在验收后清理。

分类器依据实际打包 OpenConnect 的字符串及 [OpenConnect CSTP 源码](https://gitlab.com/openconnect/openconnect/-/raw/v9.12/cstp.c) 核对；具体打包版本以候选运行时元数据为准。
