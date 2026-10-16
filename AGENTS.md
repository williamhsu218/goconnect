# GoConnect 项目边界

- 源码、测试、打包资源、维护脚本和有效的技术记录保留在仓库中。
- 项目内只保留两个已验收的分发包：`dist/current/` 正式版及 `dist/rollback/` 紧邻的回滚版。每个目录仅含一个 ZIP 和 `SHA256SUMS`；版本与构建号记录在 `dist/releases.json`。
- 普通构建不替换正式版，也不自动滚动回滚版。默认入口为 `./script/build_and_run.sh --verify` 或 `--build-only`；Swift scratch、网络组件、图标和临时应用均在系统临时目录内生成，结束、失败或中断后自动清除。
- 不在 `.build/`、`Transport/bin/`、`dist/` 或其他项目目录留下候选包、预览应用、重复 ZIP、下载分片、测试日志或第三方研究克隆。直接使用 SwiftPM 时，显式指定项目外的 `--scratch-path`，结束后清理。
- 需要交付候选 ZIP 时，用 `--export` 导出到项目外的新目录；交付或验收结束后清理。GUI 预览仅用显式 `--run`，退出后清理临时应用。
- 只有得到对应安装或发布授权、完成适用验收后，才能用 `script/release_artifacts.py retain` 更新正式版和回滚版。版本号更新或构建成功本身不构成正式验收。
- 清理先执行 `python3 script/release_artifacts.py clean` 查看清单，再加 `--apply` 执行。仅处理项目内生成文件，跳过 `.git` 与受版本控制的文件，拒绝移动正在运行的产物；废弃产物移入废纸篓。
- 不因构建或清理退出 `/Applications/GoConnect.app`、中断 VPN、改写用户配置或删除特权服务的运行组件。安装包回滚和用户数据回滚是两个独立动作。
