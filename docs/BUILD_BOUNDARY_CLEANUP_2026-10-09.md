# GoConnect 构建边界与项目清理验收

日期：2026-10-09（北京时间）。范围：项目文件夹内的 ZIP、构建缓存、测试应用、候选包及研究/下载临时产物。

## 保留结果

- 项目内 ZIP 从 80 个减至 2 个：`dist/current/GoConnect-0.11.7-macos-arm64.zip` 和 `dist/rollback/GoConnect-0.11.6-macos-arm64.zip`。
- 两个包保持原始字节，完整性、版本/build、大小及 SHA-256 均已回读；选择依据见 [版本记录](RELEASE_0.11.7.md)。
- 项目内无 `.app`、`.build/`、`Transport/bin/` 或 Python 缓存；源码、测试、打包资源和维护文档保留。
- 旧 `dist` 与其他生成产物共约 8.9 GiB 已移出项目，存放于废纸篓，可恢复；没有清空废纸篓，因此此数值不是已释放的磁盘空间。
- `python3 script/release_artifacts.py clean` 再次运行返回空清单；保留对的 `check` 通过。

## 持续构建规则

规则写入 [AGENTS.md](../AGENTS.md)，操作见 [构建说明](BUILD.md)。构建使用项目外的独立系统临时目录，默认仅构建检查，结束或失败后清除。候选包只能显式导出至项目外的新目录；已验收保留对使用独立命令更新，不随普通构建变化。

## 已执行验证

- 9 项边界回归测试通过：清理 dry run、源码/受控文件保护、项目外符号链接保护、ZIP 损坏时拒绝清理、运行中拒绝清理、回滚顺序校验、归档替换失败恢复、工具失败后临时目录清理、持久项目输出/verify 启动 GUI 拒绝。
- `./script/build_and_run.sh --verify` 完成 56 项 Swift 测试、Go race、release 打包、严格签名、动态依赖检查及内置组件隔离集成；本次临时构建目录已删除。
- 清理后再次从空 scratch 执行 `--build-only --export`，成功导出一个完整 ZIP 与校验文件，未创建项目内构建目录；外部测试导出目录和本次临时目录均已删除。
- Shell/Python 语法和 `git diff --check` 通过。
- 清理前后 `/Applications/GoConnect.app` 的 76 个文件、9 个用户配置/缓存 JSON 校验值一致；8 个当前应用/网络组件进程的 PID 和执行路径一致。当前 VPN 没有因本次任务被断开。

隔离集成没有执行需显式授权的真实服务/root TUN 测试；相关进程精确路径用例按既有条件跳过。本次没有安装新应用、切换特权服务或对外发布。
