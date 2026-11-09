# 正式发布与应用内更新流程

适用于从 1.0.1 起的后续正式版；本文件是发布操作的主入口。[AGENTS.md](../AGENTS.md) 将本流程列为项目规范；[UPDATES.md](UPDATES.md) 说明更新器行为，[BUILD.md](BUILD.md) 说明构建和产物边界。1.0.1 的公开说明见 [发布记录](https://github.com/williamhsu218/goconnect/blob/v1.0.1/docs/RELEASE_1.0.1.md)；原始安装及验收证据保留在本机私有目录，不作为公共页面附件。

**完成标准：已验收的同一份应用包在 GitHub 公开可下载，签名生产 feed 已同步，已有 GoConnect 能识别更高 build 并安全完成更新。仅创建 Release 或上传附件不满足此标准。** 普通构建不发布、不安装；接到发布请求时，把更新源同步和回验一并完成，无需为同一已授权步骤重复确认。

## 固定身份与配置

| 项目 | 当前发布约定 |
| --- | --- |
| GitHub 仓库 | `williamhsu218/goconnect` |
| tag | `v<语义版本>`，指向已验收的源码提交 |
| Bundle ID | `com.willhsu.GoConnect` |
| 签名团队 | `RX44JC39GB`，从证书 OU 核实，不取证书 CN 中的个人标识 |
| macOS 代码签名 | 现有 Apple Development 团队证书，逐层签名并启用 Hardened Runtime |
| Ed25519 钥匙串账户 | `com.willhsu.GoConnect`，与 macOS 证书密钥分别管理 |
| 配置源 | `Packaging/Updates.json`；保留既有公钥，不按版本生成新密钥 |
| 正式更新地址 | `https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml` |
| 更新源部署 | `updates` 分支根目录 `appcast.xml`，常规 fast-forward 推送 |
| 正式渠道 | 包内 `stable`；feed 的 item 不含 `sparkle:channel=preview` |
| 版本源 | `VERSION.json`，同步生成 Swift/Go 常量；数字 build 严格递增 |
| 工具版本 | 当前固定 Sparkle 2.10.1，以 `Package.resolved`、`Packaging/Updates.json` 和已核验官方 SDK 为准 |

现用方式是开发签名，**不是 Developer ID 分发签名，未经过 Apple 公证**。发布说明如实保留此边界；本机 OTA 通过不代表其他 Mac 首次安装的系统信任行为已验收。更换团队、更新密钥、公钥、账户、Bundle ID、feed URL 或签名方式前，先制定并验证旧客户端迁移路径。证书到期先在本机正常续期并复验，不用临时签名代替。私钥与密码只在本机处理，不导出进仓库或上传 GitHub。

## 1. 准备版本、源码与隔离目录

1. 核对本次发布授权与功能范围，记录工作区已有改动。先比较默认分支与最新正式 tag 的源码版本；默认分支落后时，从最新已验收 tag 创建工作分支，不能基于旧分支误构建旧版。使用独立、可审计的源码提交；有未提交工作时保留原索引和修改，不用整体 `git add -A`、reset 或 clean 拼出发布。核对公开源码不含配置、订阅凭据、日志或私有测试样本。
2. 比较现有 GitHub 正式版、生产 feed、保留包和实际已安装版。正式版语义版本及 build 均须高于已发布正式版；候选 build 必须高于生产 feed。比较前验证旧 feed 签名。相同版本重做产物或修复已发布故障时发新版本、新 build，不覆盖已公开附件或 tag。
3. 修改 `VERSION.json`，运行以下命令并提交生成文件；不要手改包内版本冒充新构建。准备本版本发布说明、README 当前版本及技术索引，一起纳入待验收提交：

```sh
python3 script/version.py --generate
python3 script/version.py --check
```

4. 候选包、展开的 App、Swift scratch、SDK、第三方源码下载和公开下载均放系统临时目录。证据另放项目外的私有目录，供清理后追溯。以下变量在同一个 shell 中使用；按实设置 `GOCONNECT_SPARKLE_BIN` 为已核验官方 SDK 的 `bin` 目录：

```sh
GOCONNECT_RELEASE_WORK="$(mktemp -d "${TMPDIR:-/tmp}/goconnect-release.XXXXXX")"
GOCONNECT_RELEASE_VERSION="$(python3 script/version.py --field version)"
GOCONNECT_RELEASE_BUILD="$(python3 script/version.py --field build)"
GOCONNECT_RELEASE_TAG="v${GOCONNECT_RELEASE_VERSION}"
GOCONNECT_RELEASE_REPO="williamhsu218/goconnect"
GOCONNECT_RELEASE_FEED="https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml"
GOCONNECT_RELEASE_ASSETS="$GOCONNECT_RELEASE_WORK/assets"
mkdir "$GOCONNECT_RELEASE_ASSETS"
```

所有后续步骤失败立即停止提升；保留失败证据。验收结束、失败或中断后，清除仅属于本次任务的临时进程、worktree 和目录，不触碰正式 App、活跃 VPN、用户数据及回滚材料。

## 2. 团队签名构建并冻结产物

明确选用现有证书。多个身份时设置 `GOCONNECT_SIGN_IDENTITY` 为本机核实的证书指纹或完整名称；团队固定如下，证书选择或团队校验失败不能退回 ad-hoc：

```sh
export GOCONNECT_SIGN_TEAM=RX44JC39GB
./script/build_and_run.sh --verify --team-sign --export "$GOCONNECT_RELEASE_WORK/export"
GOCONNECT_RELEASE_ZIP="GoConnect-${GOCONNECT_RELEASE_VERSION}-macos-arm64.zip"
cp "$GOCONNECT_RELEASE_WORK/export/$GOCONNECT_RELEASE_ZIP" "$GOCONNECT_RELEASE_ASSETS/"
ditto -x -k "$GOCONNECT_RELEASE_ASSETS/$GOCONNECT_RELEASE_ZIP" "$GOCONNECT_RELEASE_WORK/expanded"
GOCONNECT_RELEASE_APP="$GOCONNECT_RELEASE_WORK/expanded/GoConnect.app"
codesign --verify --deep --strict "$GOCONNECT_RELEASE_APP"
```

`--verify` 覆盖适用的脚本、Swift、Go race、动态依赖、运行组件及隔离网络检查，记录实际通过数量与跳过项。检查包内主程序、Runtime、Sparkle 与内嵌代码的团队和 Hardened Runtime；不得宽化 helper 的精确 CDHash 校验。核对：

- `CFBundleShortVersionString`、`CFBundleVersion` 与源码版本一致。
- `GoConnectSigningMode=team`、团队 `RX44JC39GB`，生产 Bundle ID 正确且无 QA 标志。
- `GoConnectUpdatesEnabled=true`、`GoConnectUpdateChannel=stable`，`SUFeedURL` 是固定地址，`SUPublicEDKey` 匹配既有客户端和配置源。
- `SURequireSignedFeed=true`、`SUVerifyUpdateBeforeExtraction=true`；自动下载/安装关闭，自动检查默认关闭。
- 实际最低 macOS 要求与 `build-requirements.json` 一致，不把源码部署目标当成完整包兼容性。

第三方源码必须对应**此 ZIP 展开的 App**，包括实际链接模块、Sparkle 源码和许可证，不重用旧包的附件代替。生成并校验来源清单、SHA-256、模块 h1 及适用构建材料：

```sh
GOCONNECT_RELEASE_SOURCES="GoConnect-${GOCONNECT_RELEASE_VERSION}-third-party-sources"
python3 script/prepare_release_sources.py \
  --app "$GOCONNECT_RELEASE_APP" \
  --output "$GOCONNECT_RELEASE_WORK/$GOCONNECT_RELEASE_SOURCES"
cp "$GOCONNECT_RELEASE_WORK/$GOCONNECT_RELEASE_SOURCES.tar.gz" "$GOCONNECT_RELEASE_ASSETS/"
python3 script/prepare_update.py "$GOCONNECT_RELEASE_ASSETS/$GOCONNECT_RELEASE_ZIP" \
  --sparkle-bin "$GOCONNECT_SPARKLE_BIN" \
  --output "$GOCONNECT_RELEASE_ASSETS/appcast.xml" \
  --summary "本版更新摘要。可保持 VPN 下载，确认安装重启后才断开；请先保存线路草稿。"
```

`prepare_update.py` 校验归档、包身份、团队、运行时和本机更新公钥，分别签名 ZIP 与整个 feed 并回验，不执行任何上传。核对 feed 的 version/build、最低系统、stable 渠道、enclosure 的长度及精确版本下载 URL。每次改 XML 必须重新签名；ZIP 有任何改变均重跑上述检查和相关验收。

生成只包含三个附件的清单，不将 `SHA256SUMS` 自身纳入清单：

```sh
(
  cd "$GOCONNECT_RELEASE_ASSETS"
  shasum -a 256 "$GOCONNECT_RELEASE_ZIP" "$GOCONNECT_RELEASE_SOURCES.tar.gz" appcast.xml > SHA256SUMS
  shasum -a 256 -c SHA256SUMS
)
```

冻结此后四个附件的字节、尺寸、SHA-256，以及对应的完整源码 commit。任何修改均重新生成清单，不对公开附件使用 `--clobber`。

## 3. 完成隔离真实 OTA 与适用功能验收

使用 `com.willhsu.GoConnect.UpdateQA`、临时数据和独立诊断目录，从受支持旧版 build 升至候选 build。QA 包中版本、Bundle ID 或 URL 调整后须重新团队签名并生成对应 QA 签名 feed；不能把普通无 Runtime 的 UI Preview 当 OTA 测试包，也不能修改待发布 ZIP。

QA 仅允许明确 QA 模式下的 `127.0.0.1` feed；保持签名强制开启，不修改生产 URL 或放宽生产信任。验收必须记录：

- 旧 build 实际识别更高 build，显示正确版本；真实下载、验签、安装、重启，有新 PID 且只有一个主进程，安装文件与目标包一致。
- VPN 连接中可检查、下载和验签；下载完成前不得断开连接。未保存草稿、节点测试、订阅操作和服务维护只阻止最终安装。确认安装后调用正常断开路径，等待实际停止并复核；失败暂停安装并释放新连接门禁。
- 自动检查开启后的冷启动、调度会话 true/false 边沿、无更新／失败／取消后均不得残留连接锁；新 build 启动后可连接。恢复的安装就绪状态不可静默断开 VPN，等待明确安装操作。
- 篡改 feed 被拒绝、无更新 ZIP 下载；取消/失败后恢复可用状态。涉及更新中断或恢复逻辑时验证残留状态和重启恢复。
- 涉及 TUN 创建、地址、路由或网络组件启动环境时，增加原生门禁：在无非链路本地 IPv6 地址的物理网络确认 TUN 获得预期 IPv6 地址、IPv4/IPv6 精确路由安装、真实连接和正常清理。不能用仅 IPv4 的 ProbeOnly、模拟路由或普通回环代理测试代替；所需本机授权由用户在系统界面完成。
- 涉及系统 DNS、fake-IP 或名单代理解析时，增加浏览器兼容验收：保留浏览器本地网络保护和站点权限默认值，确认公网跨来源请求不会因虚拟 ULA 地址触发本地网络授权。记录浏览器版本及真实请求结果，同时验证全局代理 DoH、名单内外的解析去向、代理失败不回落、内网真实 AAAA 与 IPv6-only 目标可达性。隔离内核检查与浏览器地址分类对照不能替代安装候选后的系统解析和实际网页验收；不得用批量授予权限、关闭保护或占用任意公网地址池绕过。
- 已有数据和生产网络不受隔离验收影响。安装授权另行按已授权范围执行：正常断开、退出、备份、替换和读回；不因发布自动退出当前正式 App 或断开 VPN。

隔离 OTA 验证更新机制，后续公开回验验证生产地址和最终字节，两者都不可省略。更换更新器或签名配置时重新覆盖对应兼容性和保护场景。涉及网络、数据、UI 的变更另完成其实际验收并如实记录跳过项。

AnyConnect UDP 历史问题按用户 2026-11-07 的确认，状态为 **部分缺陷已修复，剩余偶发超时已接受，发布非阻断**。在业务不依赖该能力的前提下，不阻断 HY2、UI、更新链路及 IPv4/TCP 公司副连接发布，不默认继续安排该项长测。尚未重新完成 UDP 实链路稳定性验收，不能标记为全部修复或 UDP 验收通过，也不能用 HY2、HTTPS 或公司 TCP 成功代替。未来修改 AnyConnect UDP 能力或业务重新依赖 UDP 时，另定适用验收；TCP、DNS、路由隔离、签名、OTA 及公开回验等其他门禁照常执行。

## 4. 推送已验收 tag，创建 draft 并核对四个附件

在已验收、可公开的源码提交上记录完整 commit，创建 annotated tag 并推送。分支默认使用 `codex/` 前缀；不顺带改工作流或扩大令牌权限。不得依赖 GitHub 自动从默认分支生成 tag：

```sh
git tag -a "$GOCONNECT_RELEASE_TAG" -m "GoConnect $GOCONNECT_RELEASE_VERSION (build $GOCONNECT_RELEASE_BUILD)"
git push origin "refs/tags/$GOCONNECT_RELEASE_TAG"
gh release create "$GOCONNECT_RELEASE_TAG" \
  "$GOCONNECT_RELEASE_ASSETS/$GOCONNECT_RELEASE_ZIP" \
  "$GOCONNECT_RELEASE_ASSETS/$GOCONNECT_RELEASE_SOURCES.tar.gz" \
  "$GOCONNECT_RELEASE_ASSETS/appcast.xml" \
  "$GOCONNECT_RELEASE_ASSETS/SHA256SUMS" \
  --repo "$GOCONNECT_RELEASE_REPO" --verify-tag --draft --latest=false \
  --title "GoConnect $GOCONNECT_RELEASE_VERSION (build $GOCONNECT_RELEASE_BUILD)" \
  --notes-file "docs/RELEASE_${GOCONNECT_RELEASE_VERSION}.md"
```

发布说明包含变更、系统要求、开发签名边界、适用验收、已知限制和网络服务升级要求。draft 阶段逐项核对：

- GitHub tag 解析后的完整源码 commit 与构建提交一致；不要用可能为分支名的 `target_commitish` 代替 commit 核对。
- 恰有四个预期附件，状态 `uploaded`；每个 `size` 和 GitHub `digest=sha256:…` 匹配冻结清单，包括 `SHA256SUMS` 自身的独立哈希。
- 名称、版本、下载路径、发布说明一致，没有未审查的额外附件；正式版 `prerelease=false`。

draft 按 tag 查询可能返回 404，可通过 `gh api "repos/$GOCONNECT_RELEASE_REPO/releases" --paginate` 查找对应 tag，再按 release ID 读回。不把一次 404 当上传失败而创建重复发布。此时不推生产 feed。

## 5. 公开 Release 并匿名回验资产

draft 核对通过后发布为 stable/latest：

```sh
gh release edit "$GOCONNECT_RELEASE_TAG" --repo "$GOCONNECT_RELEASE_REPO" \
  --verify-tag --draft=false --prerelease=false --latest
```

使用不携带 GitHub 登录凭据的公开请求，核对 `draft=false`、`prerelease=false`、`/releases/latest` 指向此版本、tag commit 和全部附件元数据。完整下载应用 ZIP、`appcast.xml`、`SHA256SUMS`，比较实际字节与冻结清单，再用官方 `sign_update --account com.willhsu.GoConnect --verify` 验证完整 feed 和 ZIP 的 enclosure 签名。

第三方源码需完成本地全包及来源审计；公开回验可以完整重下载，或使用 GitHub 全包 digest 加匿名 Range 下载与本地相同区段比较。采用后者时在记录中明确“未重新完整下载”，不能表述为全包下载验收。

公共 GET 可用 `curl -q --fail --location --proto '=https' --proto-redir '=https'`，不附 GitHub token，不使用会注入 Authorization 的个人 curl 配置。网络需要代理时只沿用已授权的本机环境或运行中的 loopback 代理；凭据只在进程内传递，不写命令行、规范、日志或报告，不更换当前 VPN 线路来完成发布。

**资产公开读回失败时不提升 feed。** 正式发布不是事务：若 Release 已公开但 feed 尚未完成，准确报告该状态，保留旧 feed，修复后继续；不可宣称应用内已可更新。

## 6. 提升签名 feed，再回验固定 URL

确认上一步全通过。推送前重新读取并验签远端当前 feed，核对没有并发发布更高 build；出现并发变更先停止、重新比较版本，不覆盖。使用独立 worktree 更新 `updates` 根目录的唯一 feed，复制**已经签名并冻结的同一份 XML**，不要编辑、重新格式化或改换行：

```sh
git fetch origin refs/heads/updates:refs/remotes/origin/updates
git worktree add -b "codex/update-feed-${GOCONNECT_RELEASE_VERSION}-${GOCONNECT_RELEASE_BUILD}" \
  "$GOCONNECT_RELEASE_WORK/feed" refs/remotes/origin/updates
cp "$GOCONNECT_RELEASE_ASSETS/appcast.xml" "$GOCONNECT_RELEASE_WORK/feed/appcast.xml"
git -C "$GOCONNECT_RELEASE_WORK/feed" add -- appcast.xml
git -C "$GOCONNECT_RELEASE_WORK/feed" commit -m "Publish signed stable feed for GoConnect $GOCONNECT_RELEASE_VERSION"
git -C "$GOCONNECT_RELEASE_WORK/feed" push origin HEAD:refs/heads/updates
```

不 force push。分支已存在时继续其历史，不每次新建无关根提交。失败先核对远端是否已成功、是否有并发发布，避免重复副作用。

匿名读回**客户端固定使用的原 URL**，不能用仅 commit URL 或附带随机参数的 URL 代替固定地址验收：

```sh
curl -q --fail --location --retry 2 --connect-timeout 20 --max-time 90 \
  --proto '=https' --proto-redir '=https' \
  --output "$GOCONNECT_RELEASE_WORK/public-appcast.xml" "$GOCONNECT_RELEASE_FEED"
cmp "$GOCONNECT_RELEASE_ASSETS/appcast.xml" "$GOCONNECT_RELEASE_WORK/public-appcast.xml"
"$GOCONNECT_SPARKLE_BIN/sign_update" --account com.willhsu.GoConnect \
  --verify "$GOCONNECT_RELEASE_WORK/public-appcast.xml"
```

另核对公开 XML 的 version/build、minimumSystemVersion、无 preview channel、enclosure URL/长度/Ed25519 签名均对应已公开的同一 ZIP，旧正式版的 build 小于目标且公钥兼容。CDN 延迟时只做有界等待和重试；未读回正确字节、签名或元数据前仍是未完成，不改 URL 绕过问题。

## 7. 记录、保留与回滚

在 `docs/PUBLISH_<版本>_<日期>.md` 记录：完整源码/tag/feed 提交、版本/build、四附件尺寸及 SHA-256、GitHub stable/latest、公开固定 URL、构建/OTA/保护/安装和数据读回范围、已知限制及证据位置。原始日志、截图和机器可读回执放项目外私有目录；无实际生产安装时不写成“已安装”。同步 README 当前版本、发布说明及技术索引，避免手动替换说明覆盖应用内更新入口。

完成适用验收后，用 `release_artifacts.py retain` 将本次应用 ZIP 保留为 current、前正式包为 rollback，再执行 `check`。两者仅含各一个 ZIP 与 `SHA256SUMS`，不把源码包或 feed 放进 `dist`。保留实际替换前应用及独立数据备份证据，清除本次临时 worktree、下载和候选包；不能清掉正在运行的应用或服务组件。

```sh
python3 script/release_artifacts.py retain \
  --current /absolute/accepted/GoConnect-new-macos-arm64.zip \
  --rollback /absolute/previous/GoConnect-previous-macos-arm64.zip
python3 script/release_artifacts.py check
```

撤回更新时，不让生产 feed 指向草稿、丢失附件、未经验证的包或更低 build。已公开 ZIP 和 tag 不覆盖，优先用新版本、更高 build 发布修复并重走全流程；紧急停止提供某次更新时，用合法且重新签名的停止分发 feed 并验签回读，不能假定清空 feed 会卸载已下载的更新。应用、本机 helper 与用户数据分别回滚；数据回滚需独立授权与验证，不能随应用降级覆盖用户新改动。

## 发布完成检查表

- [ ] 授权及范围明确，版本/build 递增，源码/tag/候选包一致，无私有数据。
- [ ] 团队签名、更新公钥、stable 配置、最低系统与嵌套 Runtime 检查通过。
- [ ] 适用构建/功能检查、隔离真实 OTA、互斥保护与篡改拒绝通过，有实际证据。
- [ ] 四附件冻结；draft 元数据、远端 tag commit、尺寸与完整 SHA-256 匹配。
- [ ] 正式 Release 已公开并为 latest，匿名下载及 ZIP/feed 验签通过。
- [ ] 同一签名 XML 已推送 updates；固定生产 URL 匿名读回一致，build/enclosure/签名正确。
- [ ] 发布记录、版本说明、dist 正式/回滚对和清理完成；安装、设备及已知限制据实报告。
