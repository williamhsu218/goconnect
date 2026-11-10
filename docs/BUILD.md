# 从源码构建

本说明面向熟悉终端的 macOS 用户，适用于已集成更新器的源码。当前正式版对应 [v1.0.15 tag](https://github.com/williamhsu218/goconnect/tree/v1.0.15)；复核正式版时先检出该 tag，再执行本文命令。项目由 SwiftUI 界面和 Go 网络组件组成。

当前开发候选为 **1.0.16 / build 76，未生产安装或发布**，新增 App 包外 Helper 与内置 CLI 的受限启动链覆盖（见 [实现和隔离验收](APP_CHILD_ROUTING_2026-11-10.md)）；正式版仍为 1.0.15 / 75。本候选，包含更新安装空闲状态与名单 DNS 地址冲突修复，分流审查后的域名/IP 规则匹配修复，以及既有直连地址导入功能。构建与安装、发布的实际结果见对应发布记录；正式/回滚包仍以 `dist/releases.json` 为准。普通构建不会替换它们。

## 准备工具

安装 Xcode Command Line Tools 或完整 Xcode，以及 Homebrew。项目需要支持 Swift 5.10 或更新版本的工具链，Go 版本要求见 `Transport/go.mod`。

```sh
xcode-select --install
brew install go openconnect
```

当前检查环境：Apple Silicon、macOS 27、Swift 6.4、Go 1.26.6。Homebrew 的组件版本会变化；打包结果会记录实际版本和最低 macOS 要求。较老系统或 Intel 构建需要单独验证。

本版使用 `script/mihomo.lock.json` 锁定 Mihomo v1.19.30 源码归档和 GoConnect 补丁的 SHA-256，由 `script/build_mihomo.py` 在临时目录编译为 `v1.19.30-goconnect2`（`with_gvisor`）。名单模式的按应用 DNS 地址重匹配依赖该补丁，打包器拒绝替换成未修复的本机/Homebrew 内核。源码、补丁、构建配方及许可证随包记录，并纳入第三方源码附件；详见 [分流修复记录](ROUTING_MODE_FIX_2026-11-10.md)。v1.0.14 tag 使用其当时的 Homebrew 内核，不包含此修复。

Sparkle、Go 模块和 Mihomo 源码有版本及完整性约束，但 OpenConnect 和其动态依赖仍取自本机安装环境，尚不是完整锁定的可复现构建。相同源码重新打包不保证与正式 ZIP 逐字节一致；依赖或工具链升级后须重新执行适用检查，不能复用旧版本的验收结论。

当前构建脚本显式使用本机 macOS SDK 和 SwiftPM native 后端。Xcode 27 的默认后端在本项目中会把部署目标写成二进制的 SDK 版本，导致系统使用旧版控件外观；native 后端可正确记录 SDK 27 并启用 Liquid Glass。native 后端当前有弃用提示，升级工具链时需重新核对 `LC_BUILD_VERSION` 和实际窗口外观，再迁移构建方式。

版本、构建号与源码部署目标统一维护在根目录 `VERSION.json`。修改后运行 `python3 script/version.py --generate` 更新提交到仓库的 Swift/Go 常量；构建入口和 CI 会检查是否同步，打包器直接读取该文件。源码部署目标同时用于 SwiftPM 与 Go 的 CGO 编译、链接参数。应用内 `Contents/Resources/build-requirements.json` 记录各二进制实际要求；Homebrew 组件要求更高时，安装包会如实采用最高要求，不能通过降低 Info.plist 来获得旧系统兼容性。

## 构建应用

```sh
git clone --branch v1.0.15 --depth 1 https://github.com/williamhsu218/goconnect.git
cd goconnect
./script/build_and_run.sh --build-only
```

Swift scratch、Go 二进制、图标、应用和 ZIP 全部在系统临时目录中生成。检查完成、失败或中断后自动删除临时目录；项目不会累积 `.build/`、`Transport/bin/` 或候选包。应用包含运行需要的网络组件，使用者不需要另外安装 Homebrew 或 Cisco 客户端。

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

`dist/` 保留的是本机验收包，仍不提交到 Git；忽略规则不表示可删除。禁止用 `git clean -fdX` 或 `git clean -fdx` 代替上述维护入口，这些命令会删除忽略的正式包和回滚包。不要把 ZIP 改成普通未忽略文件来规避此问题，那会使普通 `git clean -fd` 同样威胁发布包。

## 本地检查

```sh
./script/build_and_run.sh --verify
```

`--verify` 从校验后的源码应用内核补丁，运行其解析器边界测试、构建/清理边界回归、Swift 测试、Go race 检查、完整打包、签名及动态依赖校验，以及使用包内内核的隔离集成测试（含 DNS/CIDR/LAN/公司阻断、TCP/UDP/IPv6/TLS）。它不启动 GUI，不安装特权服务，也不主动改变系统路由。

只检查源码：

```sh
scratch="$(mktemp -d "${TMPDIR:-/tmp}/goconnect-source-check.XXXXXX")"
swift test --scratch-path "$scratch"
rm -rf -- "$scratch"
(cd Transport && go test -race -count=1 -timeout 120s ./...)
(cd Transport && go vet ./... && go mod verify)
```

`GOCONNECT_TEST_RUNTIME` 可指向应用内的 `Contents/Resources/Runtime`，让 Go 测试使用实际打包组件。需要 root、已安装网络服务或真实 TUN 的测试默认跳过。请不要在依赖当前 VPN 工作时开启这些测试；应使用专门的测试环境。

云端 CI 按发布时实际已提交并启用的工作流核对，记录对应源码提交和运行结果；Swift scratch 放在 runner 临时目录。本地配置完成不代表云端检查已生效，云端源码检查也不代表通过实际 VPN、应用分流、安装或 OTA 验收。正式发布仍执行 [发布流程](RELEASING.md) 的完整门禁。

## 签名和分发

普通本地打包默认使用 ad-hoc 签名，不需要付费 Apple Developer 账号，应用内更新禁用。后续正式发布统一按 [正式发布与应用内更新流程](RELEASING.md) 使用 `--verify --team-sign --export`，延续现有团队开发签名及更新公钥，完成 GitHub Releases、签名 feed 同步和公开回验；不得用普通本地构建替代正式包。

现用 Apple Development 团队签名不等于 Developer ID 分发签名或 Apple 公证。最终系统要求写入应用的 `Info.plist`，以所包含组件中最高的要求为准。

打包会收集许可证、组件版本和 Homebrew SBOM。生成 ZIP 不代表已经满足第三方组件的二进制再分发要求；对外发布安装包前，还需准备对应源码和适用的构建材料。当前 GitHub Releases 提供项目源码和 Apple Silicon 安装包，组件来源与许可说明见 [THIRD_PARTY.md](../THIRD_PARTY.md)。
