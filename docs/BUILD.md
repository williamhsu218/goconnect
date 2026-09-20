# 从源码构建

本说明面向熟悉终端的 macOS 用户。项目由 SwiftUI 界面、Go 网络组件和一个 C 启动器组成。

## 准备工具

安装 Xcode Command Line Tools 或完整 Xcode，以及 Homebrew。项目需要支持 Swift 5.10 或更新版本的工具链，Go 版本要求见 `Transport/go.mod`。

```sh
xcode-select --install
brew install go openconnect mihomo
```

当前检查环境：Apple Silicon、macOS 27、Swift 6.4、Go 1.26.6。Homebrew 的组件版本会变化；打包结果会记录实际版本和最低 macOS 要求。较老系统或 Intel 构建需要单独验证。

## 构建应用

```sh
git clone https://github.com/williamhsu218/goconnect.git
cd goconnect
./script/build_and_run.sh --build-only
```

输出在 `dist/`：应用、ZIP 压缩包及 SHA-256 校验文件。应用包含运行需要的网络组件，使用者不需要另外安装 Homebrew 或 Cisco 客户端。

不带参数运行脚本会构建并打开应用。若同一输出目录中的应用正在连接，脚本会拒绝替换它。

## 本地检查

```sh
GOCONNECT_REGISTER_APP=0 \
GOCONNECT_OUTPUT_DIR="$PWD/.build/verification" \
./script/build_and_run.sh --verify
```

`--verify` 运行 Swift 测试、Go race 检查、完整打包、签名及动态依赖校验，以及使用内置组件的隔离集成测试。它不启动 GUI，不安装特权服务，也不主动改变系统路由。

只检查源码：

```sh
swift test
(cd Transport && go test -race -count=1 -timeout 120s ./...)
(cd Transport && go vet ./... && go mod verify)
```

`GOCONNECT_TEST_RUNTIME` 可指向应用内的 `Contents/Resources/Runtime`，让 Go 测试使用实际打包组件。需要 root、已安装网络服务或真实 TUN 的测试默认跳过。请不要在依赖当前 VPN 工作时开启这些测试；应使用专门的测试环境。

仓库附带 [GitHub Actions 配置模板](github-actions.yml)，只执行源码测试和静态检查。此次发布账号没有 workflow 写入权限，因此未启用云端自动检查。维护者可在具备相应权限后将模板放到 `.github/workflows/check.yml`。源码检查不代表通过实际 VPN、应用分流或安装验收。

## 签名和分发

本地打包使用 ad-hoc 签名，不需要付费 Apple Developer 账号，但不具备 Developer ID 身份或 Apple 公证。最终系统要求写入应用的 `Info.plist`，以所包含组件中最高的要求为准。

打包会收集许可证、组件版本和 Homebrew SBOM。生成 ZIP 不代表已经满足第三方组件的二进制再分发要求；对外发布安装包前，还需准备对应源码和适用的构建材料。本仓库此次只发布项目源码。
