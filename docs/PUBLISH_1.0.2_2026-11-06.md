# GoConnect 1.0.2 发布验收

日期：2026-11-06。正式版 1.0.2，build 59，stable 渠道。

- GitHub Release：[v1.0.2](https://github.com/williamhsu218/goconnect/releases/tag/v1.0.2)，非 draft、非 prerelease，已设为 latest。
- 发布源码/tag：`507fa6df1d902b92b88ab5097bfa49d19f81a84e` / `v1.0.2`。
- 默认 main：`826d01a0a6c18f5a2f4a80443afedbb6bff8d8ae`，保留原 main 历史，以普通合并同步正式版源码及当前文档；其树与已验收发布源码完全相同：`41344ac323c9d3d1ea9cdbebb443589618149c3a`。
- [固定签名更新源](https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml)，updates 提交 `2ba1e71f3a3863be92ce08fcd8e96754aaa9adef`，正常 fast-forward 提升至 build 59。
- 应用 ZIP SHA-256：`72e41e39a2a8e06d35ca6595c13d4ec831a6c635d181c1c9d19784d8f20380f6`。

## 范围与签名

新菜单栏图标、默认关闭的当前线路网络速率、下行与上行／仅下行／仅上行三种方向。此前原生界面验收与持久化、过期、零值、极值、浅深色证据见 [菜单栏记录](MENUBAR_RATE_2026-11-06.md)。本轮没有改变连接、路由或 VPN 核心功能；相对 v1.0.1 的 Transport 改动只有生成的版本常量。更新准备脚本支持可选摘要，整个 XML 仍在生成后签名并回验。

沿用团队 RX44JC39GB 的 Apple Development 签名、逐层 Hardened Runtime，以及 GoConnect 原有 Ed25519 钥匙串密钥。既有客户端与候选包的公钥、固定 feed 地址一致。未采用 Developer ID，未做 Apple 公证，不扩大本机验收至其他 Mac 的首次安装信任。

## 验证结果

- 完整签名构建通过：134 项 Swift 测试、39 项 Python 测试、Go race、严格嵌套签名、动态依赖与适用隔离网络集成检查。特权安装服务和真实 TUN 测试未在生产环境执行。
- 隔离 UpdateQA 从实际已安装的 1.0.1/build 58 副本完成真实下载、验签、安装及 Sparkle 重启至 1.0.2/build 59。新 PID、单主进程、严格签名与目标文件一致均有读回。VPN 与未保存草稿阻止检查；篡改 feed 被实际客户端拒绝且未下载 ZIP；取消后检查按钮恢复。
- 四个附件的 GitHub 上传状态、大小和完整 SHA-256 digest 匹配冻结清单。匿名完整下载应用 ZIP、appcast.xml 和 SHA256SUMS，字节与官方 Sparkle 验签通过。公开 latest 已指向 1.0.2。
- 对应实际应用的第三方源码全包本地审计通过：138 条来源校验、122 个实际链接 Go 模块 h1、Sparkle 源码和许可。公开回验核对 GitHub 全包 digest，并匿名获取前 64 KiB 比较；未重新完整下载 216 MB 源码附件。
- 固定生产地址经普通匿名请求和无 Cookie／凭据／本地缓存的 Foundation 请求，均返回 HTTP 200、版本 1.0.2/build 59，匹配已签名附件并通过官方验签。刚推送时遇到旧 CDN 缓存，已等待刷新并确认解决；无查询参数或生产地址改动。
- 主工作区 Sources 与 Packaging 字节匹配验收源码，原 Git 索引及既有未提交工作保留。公开源码没有带入本地私有测试样本，也没有修改 GitHub workflows。

## 安装、数据与保留包边界

本轮只发布，由用户在正式 GoConnect 内执行更新。交付读回时，正式 App 仍是 1.0.1/build 58，163 个应用文件、原主进程及运行组件 PID/启动时间、四份受保护配置文件均保持不变；未退出 App、未断开 VPN、未替换特权服务。

使用时先正常断开 VPN 并保存线路草稿，再执行 GoConnect 菜单或设置中的“检查更新”。安装后如诊断页提示网络组件有新版本，按正常授权流程升级；本轮未代替用户升级服务。

`dist/current/` 仅保留 1.0.2/build 59 的 ZIP 与 SHA256SUMS；`dist/rollback/` 仅保留 1.0.1/build 58。版本、字节数及哈希记录于 `dist/releases.json`，保留检查通过。应用回滚与用户数据回滚独立处理。

AnyConnect 真实 UDP 稳定性按用户决定跳过，仍为未修复、未重新验收的已知限制；隔离 UDP、HY2 或 HTTPS 成功不代表该能力通过。菜单栏合成速率验收不代表实链路测速，速率也不是整台 Mac 的总网速或服务商剩余额度。

## 证据与清理

私有证据目录：`/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-1.0.2-122522/`。

主要记录：`source-freeze.json`、`candidate-package.json`、`full-build.log`、`third-party-validation-1.0.2.json`、`qa-ota-readback.json`、`qa-tamper-readback.json`、`public-release-readback.json`、`public-feed-readback.json`、`native-feed-verified.json`、`main-readback.json`、`delivery-final-readback.json`、`cleanup.json`。实际界面证据包括 `qa-update-offer.jpg` 与 `qa-upgraded.jpg`。

本次专属 QA App 已正常退出；仅停止本次 localhost 测试服务器并清除 QA 偏好。临时 worktree、构建与导出物、SDK、第三方下载和测试数据均已从系统临时目录清除，证据及有效发布/回滚包保留。发布操作由 Codex 完成，本轮未另调用 Claude、Agy 或子 agent；此前菜单栏协作记录仍保留原证据口径。
