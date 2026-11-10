# GitHub 0.11.16 预发布记录（2026-11-05）

已公开发布：[GoConnect 0.11.16 / build 56](https://github.com/williamhsu218/goconnect/releases/tag/v0.11.16)。

标签 `v0.11.16` 指向 `3b0e813af29449892a31413267fb83996fa612b4`，源码分支为 `codex/release-0.11.16`。发布快照保留远端原有 GitHub 工作流；原工作分支和暂存区保持原状。公开测试使用文档保留地址，不上传私有验收数据。

| 附件 | 字节 | SHA-256 |
| --- | --- | --- |
| GoConnect-0.11.16-macos-arm64.zip | 30539549 | `6c3eb4b6be76ea53c144479c62f2e275c8727bc4aaa00b7822351485944611ba` |
| GoConnect-0.11.16-third-party-sources.tar.gz | 199919705 | `93ab565bfee23aefc2ea542dbda25440aed2e4f9cafd465afd51273830e1ed43` |
| SHA256SUMS | 211 | `1074517e670f5e678a80e4f1d2e03f1159a61694e6430ecada522bb7c0561f01` |

三个附件均由 GitHub 返回 uploaded 状态，大小和完整文件摘要与本地一致。第三方源码附件含 15 份上游源码/补丁及 122 份实际链接的 Go 模块源码，分别对照 SBOM SHA-256 和二进制 h1 校验。

安装包和校验文件已匿名完整下载，安装包 SHA-256 与上传前一致；解压条目的 76 个普通文件与已验收安装版一致。第三方源码包验证了匿名 64 KiB 范围读取及 GitHub 完整文件摘要，未将其描述为完整二次下载。

完整构建门禁通过，包含 Swift 91 项、同步服务 9 项、其余脚本回归、Go race、签名/依赖及隔离集成。公开测试夹具另通过同步服务 9 项和 Swift Yunyoo 8 项测试。当前应用、路由配置和原网络进程与发布前一致。

AnyConnect 实际 UDP 稳定性门禁仍未通过，因此本版标记为预发布。`dist/current` 与 `dist/rollback` 继续保留原正式版和回滚版；没有将本次发布当作新的稳定版验收。

私有发布与下载证据：`/Users/willhsu/.codex/visualizations/2026/10/27/01a17c14-cb82-77c2-b350-8767b49e4950/github-release-20261105`。临时源码工作树、源码下载、构建 scratch、上传副本及下载副本在完成验收后清理。
