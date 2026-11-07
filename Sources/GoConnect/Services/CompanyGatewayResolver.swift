import Foundation
import Darwin
import GoConnectCore

enum CompanyGatewayResolver {
    static func reservation(_ company: CompanyConnectionConfiguration, configuration: Configuration) async throws -> CompanyReservation {
        let value = try company.validated(in: configuration)
        guard let id = value.companyProfileID, let profile = configuration.connections.first(where: { $0.id == id })?.profile,
              let url = URLComponents(string: try profile.validatedServer()), let host = url.host else {
            throw CompanyConnectionError.invalid("公司网关配置无效。")
        }
        let ips: [String] = try await Task.detached {
            var hints = addrinfo(); hints.ai_family = AF_INET; hints.ai_socktype = SOCK_STREAM; hints.ai_protocol = IPPROTO_TCP
            var result: UnsafeMutablePointer<addrinfo>?
            guard getaddrinfo(host, nil, &hints, &result) == 0, let first = result else {
                throw CompanyConnectionError.invalid("无法预解析公司网关，请检查服务器地址。")
            }
            defer { freeaddrinfo(first) }
            var found: [String] = [], cursor: UnsafeMutablePointer<addrinfo>? = first
            while let item = cursor {
                var buffer = [CChar](repeating: 0, count: Int(NI_MAXHOST))
                if getnameinfo(item.pointee.ai_addr, item.pointee.ai_addrlen, &buffer, socklen_t(buffer.count), nil, 0, NI_NUMERICHOST) == 0 {
                    let ip = String(cString: buffer)
                    if !found.contains(ip) { found.append(ip) }
                }
                cursor = item.pointee.ai_next
            }
            guard !found.isEmpty, found.count <= 4 else { throw CompanyConnectionError.invalid("公司网关须有 1–4 个可批准的 IPv4 地址。") }
            return found
        }.value
        return CompanyReservation(networks: value.networks, domains: value.domains, dnsServers: value.dnsServers,
                                  gatewayHost: host, gatewayPort: UInt16(url.port ?? 443), gatewayIPs: ips)
    }
}
