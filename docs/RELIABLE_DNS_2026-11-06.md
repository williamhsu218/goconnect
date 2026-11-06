# HY2 reliable DNS

Implementation introduced in candidate 1.0.7 / 66, based on accepted v1.0.6; formal release uses build 67 so installed candidates can receive the signed update. The runtime behavior is unchanged between these builds.

HY2 UDP payloads use unreliable QUIC datagrams. This change enables the node core's DNS resolver for HY2 and routes public UDP/53 flows to Mihomo's DNS outbound. The resolver uses certificate-validated HTTPS through `Selected` to Cloudflare and Google. HTTP/3 is disabled. Node bootstrap, local/reverse domain lookup and macOS system DNS remain native. Explicit private destination exclusions retain their existing selected-node path; the outer routing/PF configuration is unchanged. Other UDP and non-HY2 protocols retain their existing behavior. Mihomo still chooses UDP routing per source socket; this change does not redefine its NAT semantics.

The bundled-core fixture verifies HTTPS resolution through a selected SOCKS proxy with distinct, uncached queries. It checks independent private DNS and non-DNS UDP flows do not reach DoH; startup is excluded from query timing. It does not touch system DNS/PF/TUN. The test uses a local TLS fixture with verification disabled only in test configuration; production endpoints use normal certificate verification.

Validation: project `--verify --team-sign` passed, including 142 Swift tests, Go race checks, bundled-core and transport integrations. In an isolated physical-interface live probe, configuration produced by the candidate passed 40/40 distinct DNS queries and HTTPS 204 on each of the two configured HY2 ports. No application installation, active VPN interruption, helper upgrade, public release or production feed change was performed during these checks. Installed native-route acceptance remains pending.

The preceding results describe the original candidate validation, not the later publication outcome. Private raw evidence is retained outside this repository; no node credentials or user configuration are distributed.

References: [Mihomo DNS outbound](https://wiki.metacubex.one/en/config/proxies/dns/) and [DNS configuration](https://wiki.metacubex.one/en/config/dns/).
