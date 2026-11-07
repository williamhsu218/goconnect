# 临时公司通道测试器

用于在保留已安装 GoConnect、当前 VPN 和网络服务的前提下，测试一条 AnyConnect 公司通道。它是独立用户态测试工具，不是正式 App 的更新，也不安装第二个系统 TUN。

## 启动

在包含测试器源码的工作分支中运行：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 script/company_tunnel_test.py \
  --profile '<已有 AnyConnect profile UUID>' \
  --target '<已授权的公司 IPv4>' \
  --dns-name '<已授权的公司完整域名>' \
  --evidence '<项目外的私有证据目录>' \
  --run
```

目标限定一个明确 RFC1918 IPv4 `/32`。证据目录须为当前用户所有、0700。脚本只读正式配置，从指定 profile 取得服务器、账号和连接组；不读取钥匙串密码。

构建、Runtime 副本、应用和不含密码的 0600 启动配置均在系统临时目录。应用使用独立 bundle ID `com.willhsu.GoConnect.CompanyTunnelTester`，以现有 RX44JC39GB 团队签名。Swift 严格检查配置的 POSIX 规范路径、0600/UID/nlink，以及应用内 transport 可执行文件。

启动前先解析并批准有限的公司网关 IPv4，使用 `IP_BOUND_IF` 绑定物理网卡，回读 socket 绑定值并核对源地址。TCP 预检失败直接停止，不改为经主 VPN 串联，不修改默认/主机路由。

## 本机认证和生命周期

在原生窗口输入公司密码并点击“连接并测试”。密码仅通过 stdin 交给本次测试进程，随后清空输入框；不进入参数、环境变量、配置、证据日志或源码。实际 VPN 服务器仍严格校验证书；仅本机受限代理用临时随机令牌进行 Basic 认证。

公司角色使用 OpenConnect `--script-tun --no-dtls` 和用户态协议栈。稳定的回环 SOCKS/DNS 监听器仅属于此测试进程，不调用正式服务接口，不获得捕获锁、不执行任何系统路由或 SCDynamicStore DNS 写入。每次 Unix 后端连接核对 UID、内核 PID/start-time 和自身 transport 精确 CDHash。

停止、退出或 stdin EOF 会撤销本轮公司流，再停止所属 session/OpenConnect。缺少清理确认、超时或异常退出会锁定重连，不能显示“清理通过”。GUI 只对本次 Process 做有界终止；启动脚本若仍发现本次临时 Runtime 进程，会保留材料并报告清理未完成。

## 探测及证据口径

- 仅探测批准 `/32` 的 TCP 80、443、445；没有端口扫描范围扩大、登录提交、SMB 认证或文件读写。
- 从公司 VPN 的 ready 信息取得 DNS，以经过校验的公司后端 TCP 53 查询指定域名的 A/AAAA。不使用系统 DNS、主 DoH 或 UDP 回退。校验事务 ID、问题、RCode、回答归属、类型/Class 与有界 CNAME 链。收到有效报文、获得地址和业务可用是三项不同结果。
- 域名的 A 回答匹配批准 `/32` 时，才尝试该 IP 上的域名 HTTP/HTTPS HEAD；保留 Host/SNI 与证书校验，禁用环境代理、Cookie 和自动重定向。任何 HTTP 响应只证明对应传输路径可达，401/403/5xx 不代表业务登录或工作流通过。
- DNS 返回其它地址不会扩大公司作用域；该地址不会被自动连接。AAAA 仅观察结果，不启用公司 IPv6 业务。
- 完成后写入项目外的私有 attempt JSON；包含传输/DNS/端口/HTTP 状态与清理结果，不包含密码或代理令牌。

本工具不能替代真实系统透明双 VPN、原生系统 DNS 选择/缓存、Finder SMB 挂载、睡眠和正式 OTA 验收。公司 MFA、浏览器 SSO 和私有证书认证仍受现有 AnyConnect 传输模块能力限制。AnyConnect UDP 未修复、未重新验收。
