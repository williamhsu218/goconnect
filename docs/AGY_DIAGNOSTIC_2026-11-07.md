# Agy 调用诊断与限定源码复测

日期：2026-11-07。范围：诊断上一轮双连接方案中的 Agy 调用失败；不实施产品功能、不构建、不改变 VPN、用户配置或全局 Agent 设置。

## 结果

本轮使用原定 `gemini-4-argon-medium` / `medium`，新会话、plan 模式与 sandbox。模型清单仍提供该精确 ID。三个有界诊断均完成，不等于原整模块审查重新通过。

| 诊断 | 输入与结果 | 可证明的范围 |
| --- | --- | --- |
| MCP 最小只读任务 | 临时目录中仅有一份包含随机标记的文件；`status=SUCCESS`、`isError=false`，标记原样匹配 | 本次调用能读取正确工作区；没有重现文件不可访问或认证失败 |
| 同参数 CLI 流式对照 | 同一文件、相同模型/档位/plan/sandbox；约 54.84 秒完成，退出码 0、`status=SUCCESS`；`init.cwd` 与请求目录匹配，`init.model=gemini-4-argon-medium` | Agy CLI 自报的模型与工作区匹配；有实际文件读取工具完成事件。该回执不是底层供应商的独立证明；未返回独立 effort 字段 |
| 小范围真实源码 | 从正式源码基线 `5837d90` 提取 3 份固定片段，共 8752 字节；新会话返回 `status=SUCCESS`、`isError=false`；逐项核对引用和行号 | 内核为 `bin/mihomo`（`transparent_session.go:52`）；`RemoteNetworks` 指向 `GoConnect`（`transparent_routing.go:155`）；默认公网 DoH 带 `#GoConnect`（`managed_dns.go:188`）。片段不能证明已经支持独立公司副出口，Agy 本次也明确标为未核实 |

## 原故障定位

原两次调用均返回：`API error ... stream idle timeout (5m0s): context deadline exceeded`。首次没有结果文字，续接虽有文字仍为错误状态，且存在源码路径和内核识别错误，因此原结果继续判无效。

核对本机 `antigravity-cli-mcp-slim 0.3.0+willhsu.3` 源码：

- MCP 以 argv 数组固定传入 model/effort、plan/sandbox，并将目录作为子进程 cwd；没有在该封装中发现自动换模型逻辑。
- 原调用总时限为 600 秒，CLI 的 `--print-timeout` 为 590 秒；“5 分钟流式空闲”来自下层 Agy API 错误，不是该本地总时限到期。
- MCP 格式化返回只保留最终文本、状态和部分使用量，未保留 `init` 中的 model/cwd。这是此前回执证据不足的原因之一；本次直接保留流式事件进行了核对。
- 错误状态中附带文字不能升级为成功，尤其不能用未经源码核验的结论继续分工实施。

**可以确认的是流式 API 层超时；为什么原任务会连续五分钟无数据，尚未定位到更底层。** 新会话的小任务和限定源码任务均恢复成功，当前没有模型不存在、限额耗尽、认证失败或工作区读错的错误证据。不能仅凭复测成功，反推原时刻的网络状态；也不能断言一定是上下文过大或模型能力问题。

## 后续协作方式

后续先分小模块读取固定文件，要求真实路径/引用，再提交对应模块的结论；每段保留终态、模型/目录回执及 Codex 复核。失败会话中的文字不直接续用。大范围审查如果再次超时，再依据完整流式事件定位，而不是只加总超时时间或替换模型。

原双连接方案中 Agy 整模块审查仍未完成；本轮通过的是调用诊断与上述有限源码事实，不代表企业路由、DNS、防回落或真实双 VPN 已验收。

依据：[Agy 官方 headless 协议](https://antigravity.google/docs/cli/headless/)明确区分 `init/step_update/result` 和终态 `SUCCESS/ERROR`；本轮同时核对了本机接口实现与实际事件。

原始证据位于项目外私有目录 `/Users/willhsu/.codex/visualizations/2026/11/07/goconnect-agy-diagnostic/`。临时读取文件与源码片段在诊断结束后清理，正式 App 和 VPN 不受影响。
