# GoConnect 1.0.1 发布与安装验收

日期：2026-11-06。版本 1.0.1，build 58，stable 渠道。

- 正式 Release：https://github.com/williamhsu218/goconnect/releases/tag/v1.0.1
- 公开源码：`808b14d0cd4bdc0a3b9d8c6260027a3d49a052e7`，tag `v1.0.1`。
- 签名 feed：https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml
- feed commit：`f66b2e285263e8b3278e1bc4412fbf3ef71cfb0e`。

## 完成范围

加入受保护的 GitHub/Sparkle 更新、新双路径 G 图标及版本升级。沿用用户指定的 QuotAI 签名方式：团队 RX44JC39GB 的 Apple Development 开发签名、Hardened Runtime；GoConnect 独立 Ed25519 钥匙串密钥。未采用 Developer ID，未做 Apple 公证。

更新默认自动检查关闭，允许用户开启；自动下载安装关闭。VPN、未保存草稿、服务维护、节点测试和订阅操作与更新互斥，安装前和退出前复核。helper 保持精确 CDHash 信任边界。

用户确认 Tokyo Japan 为 HY2 后，明确跳过 AnyConnect UDP 问题。该历史随机超时限制未修复、未复测、未判定通过；取消的补测文件不进入发布。发布范围以本次已验收 HY2、界面、数据保护、签名和在线更新为准。

## 验证与实际安装

- 完整 Swift 测试 126 项通过，最后更新提示修正后另有 6 项相关回归通过；39 项 Python、Go race、隔离网络集成和严格嵌套签名通过。
- 独立 UpdateQA App 完成真实 1.0.0 → 1.0.1 下载、验签、安装、重启；只有一个新主进程，安装内容匹配目标。篡改 feed 被拒绝且未下载 ZIP；VPN 与草稿保护有实际界面证据。
- 本机 `/Applications/GoConnect.app` 已安装 stable 1.0.1/58，165 个文件匹配最终 ZIP；helper Runtime 字节一致。Tokyo Japan 恢复后真实代理请求 HTTP 204，原配置、订阅、旧凭据保留，SQLite 检查通过且流量累计继续增长。
- GitHub 以非 draft、非 prerelease 发布，并设为 latest。四个资产的尺寸和 GitHub 完整 SHA-256 与本地清单一致；匿名完整下载应用 ZIP、feed 和 SHA256SUMS，哈希与 Ed25519 均通过。
- 第三方源码包完整本地独立审计通过：138 条来源记录、122 个实际链接 Go 模块 h1 校验、Sparkle 2.10.1 源码与许可。公开读回核对 GitHub 完整 digest，并匿名下载前 64 KiB 与本地比较；未重复整包下载。
- 固定生产 feed 匿名 HTTP 200，与已签名附件逐字节一致，官方 Sparkle 工具验签通过；版本 1.0.1/build 58，无 preview channel。

应用 ZIP SHA-256：`3b9f695d935666ef3492c341506687f26ed67937e5c96a9c73d2e901d528d0f6`。

## 协作与证据

Codex 负责实现集成、签名打包、图标、真实 OTA、安装及发布。Codex 子 agent 负责 updater 实现/状态审查、签名/helper 和第三方源码审计。Agy 按请求固定 `gemini-3.8-flash-high` / high，工具权限失败后完成一次纯文本有界审查；其回执仅为平台会话元数据和自报，缺少独立型号/强度证明。其旧安装状态风险由 Codex 核实并修正。不调用 Claude。

私有证据目录：`/Users/willhsu/.codex/visualizations/2026/11/06/goconnect-1.0.1-095523/`。

主要记录：`installed-final-readback.json`、`qa-ota-readback.json`、`qa-tamper-readback.json`、`public-release-readback.json`、`public-feed-readback.json`、`third-party-validation-1.0.1.json`、`delivery-final-readback.json`、`coordination.json`；实际界面 `installed-stable-1.0.1.jpg`。

项目分发包只保留 `dist/current` 的 1.0.1/58 与 `dist/rollback` 的前正式包 0.11.14/51。替换前已安装的 0.11.17/57 应用另存于私有证据目录 `rollback-GoConnect-0.11.17-build57.zip`，用户数据独立备份于 `data-backup/`；应用回滚和数据回滚分别处理。

原工作区未提交改动及索引保留；发布使用独立、已推送 worktree/tag。临时构建、候选包和研究材料在系统临时目录内，交付后清除。
