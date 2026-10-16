# 从源码构建

本说明面向熟悉终端的 macOS 用户。项目由 SwiftUI 界面、Go 网络组件和一个 C 启动器组成。

## 准备工具

安装 Xcode Command Line Tools 或完整 Xcode，以及 Homebrew。项目需要支持 Swift 5.10 或更新版本的工具链，Go 版本要求见 `Transport/go.mod`。

```sh
xcode-select --install
brew install go openconnect mihomo
```

当前检查环境：Apple Silicon、macOS 27、Swift 6.4、Go 1.26.6。Homebrew 的组件版本会变化；打包结果会记录实际版本和最低 macOS 要求。较老系统或 Intel 构建需要单独验证。

当前构建脚本显式使用本机 macOS SDK 和 SwiftPM native 后端。Xcode 27 的默认后端在本项目中会把部署目标写成二进制的 SDK 版本，导致系统使用旧版控件外观；native 后端可正确记录 SDK 27 并启用 Liquid Glass。native 后端当前有弃用提示，升级工具链时需重新核对 `LC_BUILD_VERSION` 和实际窗口外观，再迁移构建方式。

## 构建应用

```sh
git clone https://github.com/williamhsu218/goconnect.git
cd goconnect
./script/build_and_run.sh --build-only
```

Swift scratch、Go/C 二进制、图标、应用和 ZIP 全部在系统临时目录中生成。检查完成、失败或中断后自动删除临时目录；项目不会累积 `.build/`、`Transport/bin/` 或候选包。应用包含运行需要的网络组件，使用者不需要另外安装 Homebrew 或 Cisco 客户端。

不带参数只构建和检查，不打开 GUI。需要预览时显式使用 `--run`；预览使用临时数据目录，脚本等待应用退出后删除临时产物，中断时只结束本次预览进程。`--debug` 选择 debug 构建，`--logs` 显示日志查看命令。

需要交付候选包时，导出到项目外的**新目录**，例如：

```sh
./script/build_and_run.sh --verify --export "$HOME/Downloads/GoConnect-candidate-$(date +%Y%m%d-%H%M%S)"
```

导出仅含 ZIP 和 SHA256SUMS，不覆盖已有目录，不改变保留的正式版。交付或验收结束后清理该候选目录。旧 `GOCONNECT_OUTPUT_DIR` 持久输出参数已禁用。

## 正式版、回滚版和清理

`dist/current/` 只保留一个正式版 ZIP，`dist/rollback/` 只保留一个紧邻的回滚版 ZIP，各有一份 `SHA256SUMS`；`dist/releases.json` 记录实际包内版本、构建号、大小和哈希。项目内不再保留重复 `.app`，正式安装位置仍是 `/Applications/GoConnect.app`。

普通构建不更新这两个目录。得到对应安装或发布授权并完成适用验收后，显式选择已验收的正式包和上一个正式包：

```sh
python3 script/release_artifacts.py retain --current /path/to/accepted-current.zip --rollback /path/to/previous-release.zip
python3 script/release_artifacts.py check
```

归档命令校验 ZIP、包内标识、版本和构建顺序，完整准备新保留对后才替换目录。它不安装应用、不发布 GitHub，也不验证真实 VPN；维护者必须先完成这些适用的验收。旧保留目录移入废纸篓，替换失败时恢复。

清理先查看清单，再执行：

```sh
python3 script/release_artifacts.py clean
python3 script/release_artifacts.py clean --apply
```

清理要求保留对有效，只移走项目内生成的构建目录、独立的冗余 ZIP、Python 缓存和 `.DS_Store`。受版本控制的文件、源码、维护记录、用户数据和项目外的安装/服务组件均不在清理范围；正在运行的产物会导致清理拒绝。移出的文件在废纸篓，可恢复。

## 本地检查

```sh
./script/build_and_run.sh --verify
```

`--verify` 运行构建/清理边界回归测试、Swift 测试、Go race 检查、完整打包、签名及动态依赖校验，以及使用内置组件的隔离集成测试。它不启动 GUI，不安装特权服务，也不主动改变系统路由。

只检查源码：

```sh
scratch="$(mktemp -d "${TMPDIR:-/tmp}/goconnect-source-check.XXXXXX")"
swift test --scratch-path "$scratch"
rm -rf -- "$scratch"
(cd Transport && go test -race -count=1 -timeout 120s ./...)
(cd Transport && go vet ./... && go mod verify)
```

`GOCONNECT_TEST_RUNTIME` 可指向应用内的 `Contents/Resources/Runtime`，让 Go 测试使用实际打包组件。需要 root、已安装网络服务或真实 TUN 的测试默认跳过。请不要在依赖当前 VPN 工作时开启这些测试；应使用专门的测试环境。

仓库附带 [GitHub Actions 配置模板](github-actions.yml)，只执行源码测试和静态检查。此次发布账号没有 workflow 写入权限，因此未启用云端自动检查。维护者可在具备相应权限后将模板放到 `.github/workflows/check.yml`。源码检查不代表通过实际 VPN、应用分流或安装验收。

## 签名和分发

本地打包使用 ad-hoc 签名，不需要付费 Apple Developer 账号，但不具备 Developer ID 身份或 Apple 公证。最终系统要求写入应用的 `Info.plist`，以所包含组件中最高的要求为准。

打包会收集许可证、组件版本和 Homebrew SBOM。生成 ZIP 不代表已经满足第三方组件的二进制再分发要求；对外发布安装包前，还需准备对应源码和适用的构建材料。当前 GitHub Releases 提供项目源码和 Apple Silicon 安装包，组件来源与许可说明见 [THIRD_PARTY.md](../THIRD_PARTY.md)。
