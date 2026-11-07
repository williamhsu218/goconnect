# 公司通道临时实链路验收

日期：2026-11-07（Asia/Shanghai）。测试器源码提交 `6d3c293`，工作分支 `codex/dual-vpn-test-20261107`。使用已有 AnyConnect 公司 profile，由用户在独立原生窗口本机输入密码；正式 GoConnect 仍为已安装的 1.0.8。本记录不包含公司域名、网关、账号或内网地址；实际地址与原始报告仅留在项目外私有证据中。

## 实测结果

| 项目 | 结果和证据边界 |
| --- | --- |
| 副连接外层 | `en0`，socket 的 IP_BOUND_IF 回读及源地址核对通过；未自动回退到主 VPN 串联，无新增系统路由 |
| 公司认证 | 两轮均完成真实 AnyConnect/CSTP 认证，Unix 后端 UID/PID/start-time/CDHash 校验通过 |
| 用户指定公司 IP | TCP 80 和 445 可达；443 返回不可达。只确认端口连通，没有 SMB 登录、挂载或文件读写 |
| 公司 DNS | 从副 VPN 取得公司私有 DNS，经副通道 TCP53 查询用户指定完整域名，A 回答成功返回另一公司私有 IPv4；AAAA 为 NOERROR/NODATA。没有借用系统或主线路 DNS 回退 |
| 域名网页 | 将已授权域名解析出的单个 IPv4 作为第二轮 `/32`，重新本机认证；HTTP 和 HTTPS HEAD 均返回200，HTTPS证书验证保留，没有Cookie、登录、表单或自动重定向 |
| 清理 | 两轮报告均 `cleanupConfirmed=true`；窗口结束后临时 App、Runtime、无密码启动配置和会话目录已移除 |
| 主链路 | 并行公网 HTTPS 对照184次全部204，无失败。探测间隔约2秒；第一轮会话窗口内有1个成功样本，第二轮短于采样间隔，不能声称每个毫秒均测量到 |
| 正式环境保护 | 正式 App 二进制、configuration.json、subscriptions.json 哈希前后一致；正式进程PID保留；系统DNS输出、接口清单及主路由IPv4/IPv6条目一致；服务仍ready+busy |

两轮短请求约一秒完成，1秒统计采样未形成有效流量计数；不将零计数解释为未通过隧道，也没有据此宣称速率或长期稳定性通过。实际连通性依据为已验证 Unix 后端的 TCP 建连、公司 TCP DNS 的正确回答，以及经指定公司 IP 返回的 HTTP/HTTPS 响应。

## 实现与检查

入口与使用方式见 [临时测试器说明](COMPANY_TUNNEL_TESTER.md)。新增命令只有临时用户态 `company-test` / `company-test-preflight`；没有扩展正式服务 action，没有安装或替换 App/helper，没有更新版本、tag、Release或feed。

Go 全套 `go test -race -timeout 60s ./...` 通过。新增检查覆盖单IP/物理接口/网关限制，DNS错误事务、问题、Type/Class、无关回答、CNAME链/循环/NXDOMAIN/SERVFAIL/NODATA及网页scheme拒绝。原生 Swift6/macOS14/warnings-as-errors 构建、团队签名与实际认证/结果窗口验收通过。

macOS Foundation 会把已存在 `/private/var` 文件的 URL 规范化成 `/var` 别名，首次窗口因此拒绝了自身配置，且没有发起认证。已改用 POSIX `realpath`，保留 O_NOFOLLOW/fstat/0600/UID/nlink 以及应用内可执行文件限制，修复后两轮实测通过。

## 协作及审查

- Codex：用户态CLI、物理出口、DNS/HTTP探测、临时构建签名脚本、实测及环境保护核对；Codex子agent实现原生认证/停止窗口，总控审核修正。
- Claude Code：请求 `claude-opus-5-5 / high`，型号平台回执匹配，effort未回执；只读审查认证、取消与退出，指出清理假成功、stderr管道、取消后完成、SIGPIPE等问题，已修复或以保留材料/锁定重连处理。
- Agy：请求 `gemini-4-argon-medium / medium`，review_agy SUCCESS，MCP未提供实际model/effort字段；指出DNS回答归属/类型/Class/CNAME与错误忽略等问题，总控修复并加入测试。HTTP响应与业务可用明确分开，没有为追求200自动追加登录或重定向。

原始证据：`/Users/willhsu/.codex/visualizations/2026/11/07/goconnect-company-live-test/`，包含两份私有 attempt JSON、物理预检、主链路对照、保护快照、构建/测试及协作回执。不提交原始公司网络信息或凭据。

此结果证明当前主 VPN 存续时，临时用户态公司 TCP 通道和公司 DNS 实链路可并行工作；它不代表正式 App 的系统透明双连接、原生系统DNS接管/缓存、Finder SMB 挂载、UDP、睡眠、崩溃恢复或OTA已放行。
