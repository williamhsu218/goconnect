# GoConnect 1.0.10 发布与更新源回验（2026-11-07）

GitHub 正式 Release、签名生产更新源及当前正式客户端的更新提示均已回验。本轮将公司副连接改为设置中按需启用，默认隐藏连接页入口；已有启用配置保持可见，关闭保留原配置，运行中禁止切换。详见 [发布说明](RELEASE_1.0.10.md) 与 [界面验收记录](COMPANY_FEATURE_VISIBILITY_2026-11-07.md)。

本轮未替换正式 App、未升级生产 helper、未重启主副 VPN。结束核对仍为已安装 **1.0.9 / build 69**，原 App 和网络组件 PID 全部保持一致，服务 `ready / busy`。配置、订阅缓存和公司 sidecar 的 SHA-256 与发布前完全一致；当前客户端已实际显示 **1.0.10 可供下载**，下载和安装留给用户决定。

## 身份与源码

| 项目 | 核实结果 |
| --- | --- |
| 版本 / build | 1.0.10 / 70 |
| 构建与 tag 源码 | `d7284a270cfbf9ac543abe8ccfbc71f97bbf78ad` |
| annotated tag 对象 | `f02de55213664af2d60d49ec50744ba879d7aa31` |
| updates 提交 | `c7a86cf9409c50865dd8191d859c26496c4a46fb` |
| Release | https://github.com/williamhsu218/goconnect/releases/tag/v1.0.10 |
| Release ID | 437454379 |
| 固定生产更新源 | https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml |
| Bundle / 团队 | `com.willhsu.GoConnect` / `RX44JC39GB` |
| 系统要求 | Apple Silicon，完整包最低 macOS 26.0 |

沿用 Apple Development 团队签名与 Hardened Runtime，不是 Developer ID 分发签名，未经过 Apple 公证。原 Ed25519 公钥、钥匙串账户、stable 渠道和固定更新地址不变，保留完整 feed 与解包前 ZIP 验签。新用户自动检查默认关闭；当前用户已开启的偏好保持，自动下载和自动安装均关闭。本机验收不代表其他 Mac 首次安装的系统信任行为已验证。

## 冻结附件

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `GoConnect-1.0.10-macos-arm64.zip` | 32122841 | `89d27cfbe2c99ad2ba9c88f1b2b53dc1eb8f475288882686fa4abb1966f2e50c` |
| `GoConnect-1.0.10-third-party-sources.tar.gz` | 216709229 | `f7af481b214bc6915b29167057e85dac8538c6ce5a71974b2e9ff765669ef91a` |
| `appcast.xml` | 1556 | `45ab15723ce5a32b8f4b696c5de8d2fc2289fb5548d4d719e9b1dce632c54500` |
| `SHA256SUMS` | 287 | `7a1691f171375defc160eb5d82a8059f2b23cbdd9536e91d6535f895c1857eb9` |

四附件先上传 draft 并逐项核对 uploaded 状态、大小、GitHub 完整 digest、发布说明和 tag 的真实源码提交，再公开为 stable/latest。公开后未覆盖 tag 或同名附件。技术索引与最终发布记录另同步默认分支，发布 tag 保持指向实际构建提交。

## 构建、功能和真实 OTA

- 从独立 clean worktree 执行 `./script/build_and_run.sh --verify --team-sign --export <系统临时目录>`，退出码 0：39 项 Python、193 项 Swift、适用 Go race、打包组件及隔离集成检查通过，集成测试耗时 127.397 秒。候选复制与最终导出 ZIP 的 SHA-256 完全一致。
- 原生隔离界面已验证默认隐藏、设置手动开启、取消不启用、保存后显示、关闭后再次隐藏；读取配置确认关闭仅改变 enabled。回归覆盖活跃连接和认证保护、草稿及更新互斥、持久化失败时保持启用状态。
- 已安装服务的真实 TUN / 全局接管测试及要求独立进程的子测试按显式门禁跳过，没有中断现有 VPN 来执行。网络、DNS 和隧道逻辑未改动；不将这些跳过项写成通过。
- 以保留的 1.0.9 / 69 为旧客户端，在独立 `com.willhsu.GoConnect.UpdateQA` 中真正下载、验签、安装并自动重启至 1.0.10 / 70；QA 包分别重新团队签名，主程序文本与相应原包一致，生产 ZIP 没有修改。
- 模拟 VPN 和未保存草稿保持开启时完成实际下载；草稿阻止最终安装，主机仍为 69。解除草稿并明确确认后才正常停止模拟连接和安装。真实生产 VPN 未参与模拟。
- 自动重启 PID `50661` → `51472`；连续 12 个 0.2 秒间隔样本均只有一个新主进程。163 个普通文件与目标 QA 包逐项相同，严格签名通过。
- 新版无更新、自动检查开启后的冷启动、更新失败及取消后均可再次切换模拟连接。篡改 QA feed 被实际客户端拒绝；全程更新 ZIP GET 数保持 1，未下载被篡改更新。
- 138 项实际包关联的组件 / Go 模块来源、SHA-256 及适用 h1 校验通过。第三方源码按本次冻结 App 生成，没有以旧附件代替。

## 公开回验与保留

- 匿名读取确认 stable/latest 为 v1.0.10，四附件大小与完整 digest 匹配。匿名完整下载应用 ZIP、XML 和校验清单，逐字节及 SHA-256 匹配；官方 SDK 分别验证完整 feed 和 ZIP 的 Ed25519 签名。
- 第三方源码公开回验采用 GitHub 全包 digest 加匿名 128 KiB Range 与本地同段比较，**未重新完整下载源码包**；本地完整来源审计已经通过。
- 资产公开回验通过后才将原样签名 XML fast-forward 推送 updates，未 force push。固定 URL 的匿名 curl 及原生 URLSession 均在首次回读得到同一份 build 70 XML，SHA-256 为上表值，签名与 enclosure 的版本、下载 URL 和长度正确。
- 当前正式 1.0.9 客户端在主、副 VPN 连接中显示 1.0.10 更新提示；仅选择稍后提醒，未启动生产下载或安装，也未更改用户自动检查偏好。
- 保留包已更新为 current 1.0.10 / 70、rollback 1.0.9 / 69；各目录仅一份应用 ZIP 和 SHA256SUMS，清单检查通过。应用包回滚与用户数据回滚独立。

## 限制与证据

公司副连接在有限真实场景中已确认公司 DNS、内部网页及 Docker 平台登录可达，不能据此推断所有企业环境均已通过。公司业务限 IPv4 / TCP；SMB、完整故障隔离、睡眠恢复及退出清理仍需专门验收。AnyConnect UDP 历史限制未修复、未重新验收，不阻断本次界面范围发布。完整包仍要求 macOS 26.0，源码部署目标 14.0 不代表依赖整体兼容。

原始证据保留在本机私有目录 `~/.codex/visualizations/2026/11/07/goconnect-release-1.0.10/`，包括冻结源码与附件清单、完整检查、签名、QA 截图和进程采样、安装文件比对、draft / 公开元数据、匿名下载、固定源读回、实际客户端提示及生产保护核对。企业地址、订阅、凭据与原始业务数据不进入公开附件。

QA App、循环测试服务器及安装 helper 已退出，QA 偏好恢复至测试前状态；本轮候选、SDK、源码下载和临时 worktree 在完成回验后清理，不触碰正在运行的正式 App、网络组件或用户数据。
