# 签名与 GitHub 在线更新

GoConnect 1.0.1（build 58）开始集成固定版本 Sparkle 2.10.1。0.11.17 没有更新器，首次迁移到 1.0.1 需要正常断开并退出后替换一次应用；后续版本可在应用菜单或设置中检查并安装。

后续版本的正式发布必须执行 [正式发布与应用内更新流程](RELEASING.md)：同一份已验收团队签名包、GitHub stable/latest、ZIP 与整个 feed 的 Ed25519 签名、updates 分支同步以及固定 URL 匿名回验均是完成条件。本文描述更新器行为与维护工具，不替代发布主流程。

## 身份与密钥

使用本机已有 Apple Development 团队证书及 Hardened Runtime，为主程序、Runtime、Sparkle 框架和内嵌代码逐层签名。这是团队开发签名，不是 Developer ID 签名或 Apple 公证包。首次在其他 Mac 上安装的系统信任行为不属于本机 OTA 验收结论；不提供关闭系统安全保护的方法。

更新包和整个 appcast XML 使用独立的 Ed25519 钥匙串账户 `com.willhsu.GoConnect` 签名。私钥不导出、不进入仓库或 GitHub；`Packaging/Updates.json` 只包含公钥、固定 feed、渠道和依赖来源。

helper 保留精确 CDHash 校验。团队签名或重新构建可能改变其 Runtime revision；更新 App 后按诊断页提示正常更新网络服务。App、服务和用户数据分别管理，不能用换包代替服务或数据回滚。

## 用户体验与保护

- 自动检查默认关闭，可在设置中主动开启；自动下载和自动安装始终关闭。
- 自 1.0.3 起，检查、下载和验签可以使用当前 VPN；期间允许正常连接及断开，不把整个 Sparkle 会话当成安装锁。
- 用户确认安装重启后，才调用现有正常断开路径，等待 VPN 和网络服务实际停止再交接 Sparkle。仅交接期间暂停新连接和服务操作；断开失败则暂停安装并释放门禁，不强杀进程。
- 未保存草稿、节点测试、订阅操作及服务维护只阻止最终安装，不阻止检查和下载。处理完成后，通过“安装已下载的更新并重启”明确重试。
- 自动检查的调度会话必须处理 true/false 两个边沿；无更新、取消及失败不留下连接锁。Sparkle 的 relaunch policy 查询不得修改安装状态。
- 恢复的安装就绪状态与同 build 异常重启标记只保留退出防护，连接仍可使用；新 build 清除旧标记。恢复路径不会由计时器静默断开 VPN。
- 1.0.1／1.0.2 的检查入口仍执行旧版限制，迁移本次版本需按旧流程更新一次，或正常退出后手动替换。下载完成后不再依赖 GitHub 连通性来交接安装。
- 上述防护不承诺抵御外部强制结束进程或任意多实例文件替换竞态。

## Feed 与发布顺序

生产 feed 固定为：

`https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml`

更新附件来自本仓库 GitHub Releases 的对应版本，签名前严格核对 URL、bundle ID、公钥、团队身份、版本及本地包签名。设置 `SURequireSignedFeed` 和 `SUVerifyUpdateBeforeExtraction`，验证 feed 及解包前归档完整性。

1.0.1 使用正式更新渠道。发布验收范围为已通过的 HY2 订阅、原生界面、数据保护、团队签名及更新链路。AnyConnect UDP 长期稳定性仍是已知限制，未宣称修复或通过；不将该未验收能力扩大为正式版保证。

先上传和回验应用 ZIP、第三方源码附件及 SHA256SUMS，再提升已签名的 feed；不能先推 feed 指向尚不存在的附件。每次更改 XML 都必须重新签名。

每次正式发布包含应用 ZIP、对应第三方源码包、已签名 `appcast.xml` 和 `SHA256SUMS` 四个附件。先以 draft 核对 tag commit、尺寸及 digest，公开为 stable/latest 并匿名回验资产后，才把同一 XML 推送到 `updates` 分支。仅上传 Release 不会自动同步此 feed；尚未完成固定 URL 的字节、版本、enclosure 和签名回验时，不能宣布 GoConnect 内更新可用。完整命令、检查表和回滚边界见 [RELEASING.md](RELEASING.md)。

## 维护命令

普通临时构建保持现有行为。明确请求团队签名：

```sh
./script/build_and_run.sh --verify --team-sign --export /absolute/new-directory
```

有多个证书时使用 `GOCONNECT_SIGN_IDENTITY` 和 `GOCONNECT_SIGN_TEAM` 指定现有证书。脚本从证书 OU 读取实际 TeamIdentifier，不把证书名称括号中的个人标识当团队号；缺失或不匹配时失败，不退回临时签名。

取固定版本官方 Sparkle SDK 的 `bin` 目录，放在项目外。生成发布 feed：

```sh
python3 script/prepare_update.py /absolute/GoConnect-1.0.1-macos-arm64.zip \
  --sparkle-bin /absolute/Sparkle-2.10.1/bin \
  --output /absolute/new-feed/appcast.xml
```

该命令只验包、签名及生成文件，不执行上传、Git 提交、安装或私钥导出。新增 Sparkle 许可和源码需一起进入第三方附件，旧网络组件源码附件不能直接作为完整新附件。

## 隔离 OTA 验收

仅 `com.willhsu.GoConnect.UpdateQA` 且明确 QA 标志的签名测试包允许严格的 `http://127.0.0.1:<port>/appcast.xml`。仍要求相同的 feed 和归档签名。生产 bundle 无法使用该入口，也不接受环境变量更换生产 feed。

QA 只挂载更新控件和模拟保护状态，不挂载 VPN、订阅导入、诊断或菜单栏网络操作；数据及诊断位于独立临时目录。QA 版本迁移是更新器的真实安装验收，不代表旧 0.11.17 已具备在线更新能力。
