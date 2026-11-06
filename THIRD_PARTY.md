# Third-party components

GoConnect packages these components for local use. Their licenses remain in force.
The app bundle includes license texts and exact component versions under
`Contents/Resources/ThirdParty/`. Original components can be rebuilt from these sources:

| Component | Source | License |
| --- | --- | --- |
| OpenConnect 9.21 | https://gitlab.com/openconnect/openconnect | LGPL 2.1 |
| Mihomo 1.19.30 | https://github.com/MetaCubeX/mihomo | GPL 3.0 |
| WireGuard Go netstack | https://git.zx2c4.com/wireguard-go | MIT |
| gVisor | https://github.com/google/gvisor | Apache 2.0 |
| Go supplemental libraries | https://go.googlesource.com | BSD 3-Clause |
| vpnonly launcher pattern and PF design reference | https://github.com/kanishkdan/vpnonly | MIT (CLI and app-engine sources) |

GoConnect's application-group capture implementation is local code. Earlier
versions used the privilege-drop and responsibility-attribution launcher pattern
from vpnonly's MIT-licensed engine; the current transparent-routing package no
longer includes that launcher. Attribution is retained as `vpnonly-MIT.txt`; no proprietary
VPNonly application or website assets are included.

OpenConnect uses GnuTLS, GMP, Nettle, p11-kit, stoken, gettext, libidn2,
libtasn1, libunistring, libtomcrypt and libtommath. The packager copies the
applicable licenses and Homebrew SBOMs for the libraries actually linked.
Apple system libraries are used from macOS. The Mozilla CA bundle supplied by
Homebrew ca-certificates is copied for TLS certificate validation.

The bundled command-line programs remain separate executables. Replacing or
rebuilding a bundled component requires re-running local ad-hoc signing.
This local build is not notarized. Before redistributing binaries, supply the
corresponding source and any other materials required by each component's license.

The 0.11.16 GitHub prerelease supplies a separate
`GoConnect-0.11.16-third-party-sources.tar.gz` asset with the matching source
archives, Homebrew patches and formulas, SBOMs, licenses, and the Go module sources
recorded in the bundled executables. `sources.json` records provenance and checksums.
Maintainers can collect these materials with `script/prepare_release_sources.py`
using the accepted app and a new output directory outside the repository.

### Subscription parsing (0.6.0)

GoConnect uses `gopkg.in/yaml.v3` v3.0.1 (MIT / Apache-2.0) to read Clash YAML subscription data. Its exact version and license texts are included in the app's ThirdParty directory. Subscription nodes run through the already bundled Mihomo core as an ordinary user; Clash Verge's scripts and runtime configuration are not imported.

## Sparkle 更新框架

Sparkle 固定为 2.10.1，许可证为 BSD 风格许可；应用内附带上游完整 LICENSE（包含其第三方声明）。官方仓库：https://github.com/sparkle-project/Sparkle 。精确 SwiftPM revision 记录在 Package.resolved，源码 URL 与 SHA256 记录在 Packaging/Updates.json 及包内 ThirdParty/Sparkle-2.10.1/source.json。1.0.1 的第三方源码附件新增对应 Sparkle 源码，不能用仅覆盖旧网络组件的附件替代。
