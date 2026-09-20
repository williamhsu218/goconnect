import Foundation

/// An editor owns a value copy. Only a successful save updates the configuration.
public struct ConnectionDraft: Equatable, Identifiable, Sendable {
    public var id: UUID { connection.id }
    public let original: SavedConnection?
    public var connection: SavedConnection

    public init(editing connection: SavedConnection) {
        original = connection
        self.connection = connection
    }

    public init(copying source: SavedConnection? = nil) {
        original = nil
        connection = source ?? SavedConnection()
        connection.profile.id = UUID()
        connection.profile.rememberPassword = false
        connection.profile.name = source.map { $0.displayName + " 副本" } ?? ""
    }

    public var isNew: Bool { original == nil }
    public var hasChanges: Bool {
        if let original { return connection != original }
        return !connection.profile.name.isEmpty || !connection.profile.server.isEmpty
            || !connection.profile.username.isEmpty || !connection.profile.group.isEmpty
            || connection.profile.rememberPassword || connection.routingMode != .whitelist
            || connection.companyRoutesEnabled || connection.accessMode != .applicationProxy || !connection.automaticRemoteNetworks || !connection.remoteNetworks.isEmpty
            || !connection.applications.isEmpty || !connection.excludedApplications.isEmpty || !connection.directDomains.isEmpty
    }
}

extension Configuration {
    public mutating func save(_ draft: ConnectionDraft) throws {
        var edited = draft.connection
        edited.profile.name = edited.profile.name.trimmingCharacters(in: .whitespacesAndNewlines)
        edited.profile.server = edited.profile.server.trimmingCharacters(in: .whitespacesAndNewlines)
        edited.profile.username = edited.profile.username.trimmingCharacters(in: .whitespacesAndNewlines)
        edited.profile.group = edited.profile.group.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !edited.profile.name.isEmpty else { throw ConfigurationError.invalidName }
        guard edited.directDomains.count <= 256 else { throw ConfigurationError.invalidDomainRules }
        edited.directDomains = try edited.directDomains.map { try DomainRule($0.domain, includeSubdomains: $0.includeSubdomains) }
        guard Set(edited.directDomains.map(\.domain)).count == edited.directDomains.count else { throw ConfigurationError.invalidDomainRules }
        edited.remoteNetworks = try RemoteNetwork.validated(edited.remoteNetworks)
        if edited.companyRoutesEnabled && (edited.subscription != nil || edited.remoteNetworks.isEmpty) {
            throw ConfigurationError.invalidCompanyMode
        }
        if edited.subscription == nil { _ = try edited.profile.validatedServer() }

        if let original = draft.original {
            guard edited.id == original.id,
                  let index = connections.firstIndex(where: { $0.id == original.id }) else {
                throw ConfigurationError.invalidProfiles
            }
            guard connections[index].profile == original.profile,
                  connections[index].routingMode == original.routingMode,
                  connections[index].accessMode == original.accessMode,
                  connections[index].companyRoutesEnabled == original.companyRoutesEnabled,
                  connections[index].directDomains == original.directDomains,
                  connections[index].automaticRemoteNetworks == original.automaticRemoteNetworks,
                  connections[index].remoteNetworks == original.remoteNetworks else {
                throw ConfigurationError.profileChanged
            }
            // Application lists can be edited elsewhere; retain their latest values.
            connections[index].profile = edited.profile
            connections[index].routingMode = edited.routingMode
            connections[index].accessMode = edited.accessMode
            connections[index].companyRoutesEnabled = edited.companyRoutesEnabled
            connections[index].directDomains = edited.directDomains
            connections[index].automaticRemoteNetworks = edited.automaticRemoteNetworks
            connections[index].remoteNetworks = edited.remoteNetworks
        } else {
            guard connections.count < 64, !connections.contains(where: { $0.id == edited.id }) else {
                throw ConfigurationError.invalidProfiles
            }
            connections.append(edited)
        }
    }
}
