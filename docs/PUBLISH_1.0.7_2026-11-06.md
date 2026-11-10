# GoConnect 1.0.7 / build 67 发布验收（2026-11-06）

已完成正式发布、签名更新源同步和固定地址公开回验。GitHub stable/latest：[v1.0.7](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.7)，Release ID `436681055`。实际生产 App 1.0.7 / 66 在连接中识别了 1.0.7 / 67；保留安装确认窗口，未代用户安装或中断 VPN。

## 源码与范围

- 用户授权：检查 VPS 会话引入的 HY2 UDP DNS 修复，通过后发布 GitHub Releases；按项目规范包含签名和更新源。
- 原实现：独立 `GoConnect-DNS` 工作区，基于已验收 `v1.0.6`，本地候选 1.0.7 / 66；该会话已完成安装和正常服务授权。
- 正式构建：1.0.7 / 67，高于候选 66，避免安装候选后无法获得正式包。DNS 实现和网络行为与 66 相同。
- 构建／tag 源码提交：`f03e080b41b743bc50b7e4dae5a85cdd87bdd364`。
- 源码 tree：`eb5d7c079ce858f42e3d0c6411b6fffa3c539d81`。
- Annotated tag 对象：`1c1ca0833139aa43d7bee337954540d808bc5f2d`。
- 默认 main 合并提交：`323a5051f9fc0a67049da25724aea145d769f3d9`，tree 与 tag 一致；保留旧 main 和验收提交双亲。
- updates：`02987b3b9e300e6d53e4522b9dbd512bc3d40598`，父提交 `5425f63dde24d8e5184291af98ed2317e57376ee`，常规 fast-forward，无 force push。
- 仅 HY2 节点内部公网 UDP/53 转 DoH，经 Selected；生产 TLS 校验开启，不使用 HTTP/3。系统／内网 DNS、非 DNS UDP、非 HY2 协议和外层分流不改。
- 原工作区未提交内容和索引保留。只将已核对的 11 个本次文件同步到主工作区；技术索引保留其私有历史条目。原 DNS 工作区未重置、未提交、未清理。

## 构建、签名和运行验收

- 已提交源码执行 `--verify --team-sign`：142 Swift、39 Python、Go race、真实 bundled-core 及 transport integration 通过；最终 integration 125.693 秒。需要显式特权服务环境的两个集成场景和 child-only 测试按原规则跳过。
- 构建命令最后的 `--export` 路径存在输入笔误，导致仅最终复制步骤报 PermissionError，wrapper exit 1。此前已将打包完成的同一 ZIP 复制到本次临时区用于并行验证；此 ZIP 的编译、签名、完整回归均已完成。未重新构建或改动 ZIP；其哈希为下表值，后续严格包校验、实链路、真实 OTA、公开下载均使用此份字节。完整原始日志保留，未将 wrapper 记录伪称 exit 0。
- Team `RX44JC39GB`、Apple Development、Hardened Runtime、严格嵌套签名，生产 bundle ID、公钥、钥匙串账户及固定 feed 不变。并非 Developer ID，未做 Apple 公证。包要求 Apple Silicon / macOS 26。
- 原候选双端口 DNS 各 40/40（另一会话）；本轮正式包从真实 transport 生成配置，绑定物理 en0、仅 loopback SOCKS、不操作系统路由：端口 443 与 45392 各 26/26，无重试；各 24 个不同域名防缓存查询、2 个返回实际地址的 A/AAAA 查询；两端口 HTTPS 204。
- 当前安装版 66 的实际应用持有节点内核：22/22，无重试，包含 20 个不同域名及正向 A/AAAA；HTTPS 204，服务 ready。此证据是运行中节点内核的认证 SOCKS UDP 路径，不冒充独立应用进程经 TUN 的全量测试。
- 原生界面实际显示 Tokyo Japan 已连接；主进程 43896 保持。TUN 存在内部 IPv6 地址和既有路由。全部 11 个基线 JSON 文件哈希未变。未调整真实节点、模式、DNS 系统设置或 helper 信任边界。
- 已安装候选 66 与正式 67 的完整包字节不同；本轮未替换本机已安装 App。正式 67 如提示服务更新，应按诊断页正常授权。

## 真实隔离 OTA 与保护

使用独立 `com.willhsu.GoConnect.UpdateQA`、临时数据、127.0.0.1 签名 feed、同一团队／Ed25519 密钥；生产 ZIP 不改。

- 原正式 1.0.6 / 65 → 正式代码 1.0.7 / 67：真实下载、验签、安装；按“安装已下载的更新并重启”后自动重启，未手工冷启动，旧 PID 49078 → 新 PID 49813，单一主进程。
- 163 个正常文件与签名目标 App 逐一一致，严格签名验证通过。
- 模拟 VPN 连接和草稿同时存在时可检查／下载；尝试最终安装被草稿阻止，模拟连接保留。解除草稿后通过正常安装流程；新版可再次切换连接状态。
- 新版无更新、篡改 feed 失败及取消后无连接锁。篡改 build 为 68、未重签的 XML 被明确拒绝，服务日志没有新增 ZIP GET。
- 模拟保护与真实网络证据分开：QA 不启动真实 VPN；实际生产会话未中断。

## 冻结附件

| 文件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| GoConnect-1.0.7-macos-arm64.zip | 31696716 | `5405459fc4eb11c9e22162d3cc246a3afd98fc9faf7d01f59e80ac6ff8435114` |
| GoConnect-1.0.7-third-party-sources.tar.gz | 216709003 | `3aa56143d5801ebec8ffc428da5df87d8aaa494ef36fb97b08d32a41842f24e0` |
| appcast.xml | 1565 | `8456352bdca0a888b29225bcfbabdc39b8595c490a7111fd2e752d8d0d93a9b3` |
| SHA256SUMS | 285 | `d9a824a8d40165169eb15b19dabf72b4041140ce475cfaba13401ec9d0555454` |

第三方完整本地审核：138 条 SHA-256、122 个 Go module h1／实际二进制模块集合、15 项组件／13 项 Homebrew formula、Sparkle 源码和许可证、45 项元数据全部通过。GnuTLS 官方 www 主机下载超时后从同域无 www 地址获取；字节哈希与 App SBOM 预期完全一致，未改校验值。

## 公开发布与更新源

1. Annotated tag 指向上述验收提交；四附件 draft 全部 uploaded，尺寸和 GitHub 全包 digest 与冻结清单一致。
2. 发布 stable/latest，匿名 latest 页面正确跳转；匿名完整下载 App ZIP、XML 和 SHA256SUMS，逐字节／SHA-256 一致；ZIP 和完整 XML Ed25519 验签通过。
3. 第三方源码公开检查使用 GitHub 全包 digest 加匿名 64 KiB Range 与本地对应片段比对；未重新完整下载公开源码包。
4. 同一份签名 XML 在资产公开回验通过后才提升到 updates。固定 URL `https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml` 匿名及原生 URLSession 读回完全一致：build 67，stable，正确 enclosure URL、长度和签名。此次原生请求第 1 次即取得新字节。
5. 实际运行的 1.0.7 / 66 在 VPN 连接中检查更新，明确显示“1.0.7 (67) 可供下载，当前 1.0.7 (66)”；停留在安装确认。

## 保留、限制和证据

正式 current 为 1.0.7 / 67；紧邻 rollback 为 1.0.6 / 65。只各保留一个 ZIP 和 SHA256SUMS，`release_artifacts.py check` 通过。原候选安装和数据备份属于另一会话的私有交付，不在本次清理范围。

已知 AnyConnect 真实 UDP 历史限制未修复、未重新验收；本次 HY2 DNS 成功不代表普通 UDP 丢包、所有 DNS 策略或企业内网解析通过。Shadowrocket 和 Clash 的设备导入不属于本轮 GoConnect 发布验收。

本轮私有证据：`/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-1.0.7-release`。核心回执：`build-validation.json`、`final-live-dns-acceptance.json`、`installed-dns-acceptance.json`、`qa-acceptance.json`、`third-party-validation-1.0.7.json`、`public-release-readback.json`、`public-feed-readback.json`、`native-feed-verified.json`、`production-update-offer.json`、`production-preservation.json`。最终清理结果见 `cleanup.json`。


## 用户安装后的实机复验（2026-11-06 23:51，Asia/Shanghai）

用户完成正式更新后重新读取：`/Applications/GoConnect.app` 已是 1.0.7 / 67，163 个正常文件与正式 ZIP 完全一致，严格嵌套签名通过。实际运行网络服务为 ready/busy，当前 helper 的 17 个 Runtime 文件逐一匹配新版 App，已切换到新 revision `48b6b97dfa099f09066f809d6a6212fcea138a9f827a29efb013c630412411d0`。此结果补充前述“尚未安装”的历史交付状态。

实际界面显示 Tokyo Japan／全局模式已连接；诊断页显示“本机网络服务：已授权，可直接连接”及运行环境正常。运行中的应用节点内核已经启用 GoConnectDNS 公网 UDP/53 规则，Cloudflare／Google DoH 明确经 Selected，HTTP/3 关闭；系统 bootstrap 和内网／反向域名策略保留。

通过当前 App 持有的 HY2 节点内核进行认证 SOCKS UDP 查询，两个公网 DNS 目的地址合计 44/44 成功：40 个随机不同域名查询防止缓存掩盖问题，4 个 A/AAAA 正向查询返回实际地址，均无重试，最大耗时 404 ms。经同一当前代理的 HTTPS 请求返回 204。这是当前真实节点内核路径验证，不冒充独立应用至 TUN 的全量路由测试。11 个 JSON 配置文件在复验期间不变，未断开或切换当前 VPN。

证据：`/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-1.0.7-post-update`，包含 `acceptance.json`、原生连接截图及诊断页 AX 状态。本次确认正式 build 67 的 HY2 DNS 修复已在运行中生效。
