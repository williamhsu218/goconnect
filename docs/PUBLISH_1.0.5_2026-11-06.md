# GoConnect 1.0.5 连接修复及紧凑菜单栏发布

2026-11-06；stable 1.0.5/build 64。GitHub Release ID `436178539`，源码/tag `adc7eb812a32bd2241f80d8a4cff0ccd4c790cc7` / `v1.0.5`。main 合并 `93e1bf3fd00cb0702bf78b10bb2fd1c9d6a3edda`，树 `b2d4a27452920fc92c560deb4dd55e5362d5c5be` 与 tag 一致。签名 updates 提交 `174fc92ad3073e6814df3a464f6258fa31a5cbf2`，从 1.0.4 正常 fast-forward。

## 故障及修复验收

旧版连接 Tokyo Japan 时复现 `::/1` 新增路由精确查表失败。现场 utun100 只有 IPv4 地址，确实缺失 IPv6 地址与路由；不是查表解析误判。上游 Mihomo 1.19.30 的宿主 IPv6 检测会清除 TUN IPv6 配置。

只为 GoConnect 管理的 TUN 子进程设置官方 `SKIP_SYSTEM_IPV6_CHECK=1`。纯代理子进程、系统 IPv6 设置、helper 精确 CDHash 信任、路由所有权和精确验证保持原规则。新增实际进程环境隔离测试及真实打包 Mihomo 配置解析回归测试。

最终 build64 本机安装后，正常升级服务并连接原 Tokyo Japan（白名单）；utun100 获得 `fdfe:dcba:9875::1/126`，`0/1`、`128.0/1`、`::/1`、`8000::/1` 四条精确路由存在。实际认证代理 HTTPS 请求返回 204，主窗口已连接并读取出口 IP。前一同代码候选已完成正常断开、原路由清理、原物理/其他接口默认路由保持及重连；最终服务 Transport 文件与 App 完全一致。

本机安装最终版 163 个文件匹配签名 ZIP。配置、订阅及两份旧配额关联文件的哈希保持；流量库保留、正常继续累计。旧 App 与独立数据备份分开保存；未回写旧配置，未操作系统 IPv6 或其他 VPN 的路由。

## 菜单栏

按用户最终要求保留 MB/s 等完整单位，去掉多余空格，最多三位整数或一位小数；不会显示四位整数或 `100.0`。上下双行或单向一行均固定 77pt，关闭速率恢复本机 22pt 方形按钮。悬停和辅助功能保留完整数值。

原生 NSStatusBarButton 在常用、零值、极值、进位边界和过期数据下均读回 77pt，图像 75×22pt；原生绘制透明边界左 2.5pt、右 1.5pt。最终组件哈希匹配发布源码。测试数据仅用于布局，不是生产测速。当前 Mac 外观及缩放通过，未覆盖所有显示器组合。

## 构建、签名与在线更新

- 142 项 Swift、39 项 Python、Go race、动态依赖、严格嵌套签名及完整隔离运行组件检查通过；最终集成耗时 126.965 秒。
- 团队 RX44JC39GB 的 Apple Development 签名与 Hardened Runtime；原 Ed25519 公钥及固定生产 feed 不变。ZIP 与完整 XML 分别签名。未做 Developer ID 分发签名或 Apple 公证。
- 旧版 1.0.4/61 隔离副本下载、验签，明确正常退出后由 Sparkle 安装，再由 CUA 冷启动 1.0.5/64；163 个文件与目标一致。这里没有把手动冷启动写成 Sparkle 自动重启。新 PID、单主进程、连接模拟入口可用；无更新检查不残留连接锁。
- 篡改 feed 被实际拒绝，服务器没有 ZIP 下载，取消后入口恢复。
- 138 条来源 SHA、122 个 Go 模块 h1、15 个组件来源/13 份配方、Sparkle 源码及许可证和 45 项元数据完整通过。
- 四附件 GitHub 完整 digest、尺寸和状态匹配；匿名完整下载应用、feed、SUMS 并验签。源码附件为本地完整审计、GitHub 全包 digest 及匿名前 64 KiB 比较，没有重复全包下载。
- 固定生产 URL 匿名回验及 Foundation 原生无本地缓存策略均返回 1.0.5/64，与附件完全一致并通过验签。

AnyConnect 真实 UDP 按用户决定跳过，未修复、未重新验收；不把 HY2、HTTPS 或隔离 UDP 结果记为该能力通过。

## 资产及证据

| 附件 | 字节 | SHA-256 |
| --- | ---: | --- |
| GoConnect-1.0.5-macos-arm64.zip | 31691888 | `616206ae62de59ae047400b386c82e94b41afdf8051fb52ed30d53b6402de92f` |
| GoConnect-1.0.5-third-party-sources.tar.gz | 216709086 | `48d5e4de17f7246744a84d76ce74f42118dbdd85feda2c0f553629cb1c7548f2` |
| appcast.xml | 1522 | `726bf18ed170d3b6c25c35a1aafda17aef316ad7185539ceb4c78f40aa0784c1` |
| SHA256SUMS | 285 | `b82535c3c4df20203adcd9a0fa3f9df6d9ece11e70576c862abd09fc7ecd1e3d` |

证据：`/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-1.0.5-140142/`，重点为 `tun-route-transition.json`、`native-final-acceptance.json`、`native-final-routing.json`、`native-disconnect-cleanup.json`、`installation-final-readback.json`、`menu-final-acceptance.json`、`source-freeze.json`、`full-build.log`、`qa-ota-readback.json`、`qa-tamper-readback.json`、`third-party-validation-1.0.5.json`、`public-release-readback.json`、`public-feed-readback.json`、`native-feed-verified.json`。

发布后保留 current 1.0.5/64、rollback 1.0.4/61；后续 1.0.6 发布会按同一规则滚动。原 Git 索引及用户未提交改动保留。本轮由 Codex 实现与验证，未调用其他模型。
