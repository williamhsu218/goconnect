import XCTest
@testable import GoConnectCore

final class CoreTests: XCTestCase {
    func testAppleScriptPreservesControlCharactersAndShellMetacharacters() throws {
        let value = "App\n\r\"\\字 '$() `x`"
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
        process.arguments = ["-e", "return id of " + ShellQuote.appleScriptString(value)]
        let pipe = Pipe(); process.standardOutput = pipe
        try process.run(); process.waitUntilExit()
        XCTAssertEqual(process.terminationStatus, 0)
        let result = String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
        let expected = value.unicodeScalars.map { String($0.value) }.joined(separator: ", ")
        XCTAssertEqual(result.trimmingCharacters(in: .whitespacesAndNewlines), expected)
        XCTAssertFalse(ShellQuote.appleScriptString(value).contains("\n"))
        XCTAssertFalse(ShellQuote.appleScriptString(value).contains("\r"))
    }

    func testConnectionSwitchTracksCancellationFailureAndDisconnectCleanup() {
        func state(_ phase: String, password: Bool = false, unmanaged: Bool = false) -> VPNControlState {
            VPNControlState(phase: phase, passwordRequested: password, testOnly: false, routingCheck: false, hasUnmanagedApps: unmanaged)
        }
        XCTAssertFalse(state("idle").isOn)
        XCTAssertTrue(state("idle", password: true).isOn)
        XCTAssertEqual(state("idle", password: true), .waitingForPassword)
        XCTAssertFalse(state("idle", password: false).isOn, "Cancelling the password prompt must reset the switch")
        XCTAssertTrue(state("connecting").isOn)
        XCTAssertTrue(state("preparingService").isOn)
        XCTAssertFalse(state("preparingService").canToggle, "An installation authorization must finish or be canceled by the system dialog")
        XCTAssertTrue(state("authorizing").isOn)
        XCTAssertTrue(state("connected").isOn)
        XCTAssertEqual(state("connected", unmanaged: true), .needsRestart)
        XCTAssertTrue(state("protected").needsAttention)
        XCTAssertTrue(state("protected").isOn, "An interrupted session must remain possible to disconnect")
        XCTAssertFalse(state("stopping").isOn)
        XCTAssertFalse(state("stopping").canToggle, "Do not start another session while cleanup is pending")
        XCTAssertTrue(state("idle").canToggle)
        XCTAssertFalse(state("failed").isOn)
        XCTAssertTrue(state("failed").canToggle)
    }

    func testDiagnosticsNeverAppearAsAnEnabledVPN() {
        let awaitingPassword = VPNControlState(phase: "idle", passwordRequested: true, testOnly: true, routingCheck: false, hasUnmanagedApps: false)
        XCTAssertEqual(awaitingPassword, .diagnostic)
        XCTAssertFalse(awaitingPassword.isOn, "A diagnostic password prompt is not a VPN connection request")
        for phase in ["connecting", "testing", "stopping", "preparingCheckService", "serviceMaintenance"] {
            let state = VPNControlState(phase: phase, passwordRequested: false, testOnly: true, routingCheck: false, hasUnmanagedApps: false)
            XCTAssertFalse(state.isOn)
            XCTAssertFalse(state.canToggle)
        }
        let checking = VPNControlState(phase: "checking", passwordRequested: false, testOnly: false, routingCheck: true, hasUnmanagedApps: false)
        XCTAssertEqual(checking, .diagnostic)
        XCTAssertFalse(checking.isOn)
        let finished = VPNControlState(phase: "idle", passwordRequested: false, testOnly: true, routingCheck: false, hasUnmanagedApps: false)
        XCTAssertEqual(finished, .off, "A completed diagnostic must allow normal VPN connections again")
    }

    func testDraftSaveKeepsActiveConnectionIdentityAndLatestApplicationLists() throws {
        var config = Configuration()
        config.profile.server = "active.example"; config.profile.username = "active"
        let activeID = config.activeProfileID
        try config.add(name: "Other", copyCurrent: true)
        let otherID = config.activeProfileID
        try config.select(activeID)
        let before = config
        var draft = ConnectionDraft(editing: config.connections[1])
        draft.connection.profile.name = " Updated "
        draft.connection.profile.server = "new.example"
        draft.connection.routingMode = .global
        XCTAssertEqual(config, before, "Editing a draft must not mutate saved configuration")
        let selected = AllowedApplication(name: "VPN", bundleID: "test.vpn", path: "/Applications/VPN.app")
        let excluded = AllowedApplication(name: "Direct", bundleID: "test.direct", path: "/Applications/Direct.app")
        config.connections[1].applications = [selected]
        config.connections[1].excludedApplications = [excluded]
        try config.save(draft)
        XCTAssertEqual(config.activeProfileID, activeID)
        XCTAssertEqual(config.connections[0], before.connections[0])
        XCTAssertEqual(config.connections[1].id, otherID)
        XCTAssertEqual(config.connections[1].profile.name, "Updated")
        XCTAssertEqual(config.connections[1].routingMode, .global)
        XCTAssertEqual(config.connections[1].applications, [selected])
        XCTAssertEqual(config.connections[1].excludedApplications, [excluded])
        XCTAssertThrowsError(try config.save(draft), "A stale editor must not overwrite a newer profile")
    }

    func testNewDraftAndInvalidSaveDoNotCreatePartialProfiles() throws {
        var config = Configuration()
        config.profile.server = "first.example"; config.profile.username = "first"
        config.profile.rememberPassword = true
        config.excludedApplications = [AllowedApplication(name: "Direct", bundleID: "test.direct", path: "/Applications/Direct.app")]
        let before = config
        var empty = ConnectionDraft()
        XCTAssertFalse(empty.hasChanges)
        XCTAssertThrowsError(try config.save(empty))
        empty.connection.profile.name = "Incomplete"
        XCTAssertThrowsError(try config.save(empty))
        XCTAssertEqual(config, before)
        let copy = ConnectionDraft(copying: config.connections[0])
        XCTAssertTrue(copy.isNew)
        XCTAssertFalse(copy.connection.profile.rememberPassword)
        XCTAssertNotEqual(copy.id, config.activeProfileID)
        try config.save(copy)
        XCTAssertEqual(config.activeProfileID, before.activeProfileID)
        XCTAssertEqual(config.connections[0], before.connections[0])
        XCTAssertEqual(config.connections[1].excludedApplications, before.excludedApplications)
        XCTAssertThrowsError(try config.save(copy), "Saving one draft twice must not duplicate its identity")
    }

    func testProfileRejectsCredentialsAndInsecureSchemes() throws {
        var profile = VPNProfile(); profile.username = "test"
        for server in ["http://vpn.example", "https://user:secret@vpn.example", "https://vpn.example?token=x", "-S /bin/sh", "https://vpn.example\n--no-cert-check"] {
            profile.server = server
            XCTAssertThrowsError(try profile.validatedServer(), server)
        }
        profile.server = "vpn.example:8443/work"
        XCTAssertEqual(try profile.validatedServer(), "https://vpn.example:8443/work")
    }
    func testPersistenceDoesNotContainCredentialsAndRejectsFutureSchema() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: dir) }
        let file = ConfigurationFile(url: dir.appendingPathComponent("settings.json"))
        var config = Configuration(); config.profile.server = "vpn.example"
        try file.save(config)
        XCTAssertEqual(try file.load(), config)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: file.url)) as? [String: Any])
        let connections = try XCTUnwrap(json["connections"] as? [[String: Any]])
        let profile = try XCTUnwrap(connections.first?["profile"] as? [String: Any])
        XCTAssertNil(profile["password"])
        let attrs = try FileManager.default.attributesOfItem(atPath: file.url.path)
        XCTAssertEqual((attrs[.posixPermissions] as? NSNumber)?.intValue, 0o600)
        config.schemaVersion = 99; try file.save(config)
        XCTAssertThrowsError(try file.load())
    }
    func testConnectionsKeepIndependentSettingsAndWhitelist() throws {
        var config = Configuration()
        let firstID = config.activeProfileID
        config.profile.server = "first.example"; config.profile.username = "first"
        config.profile.rememberPassword = true
        config.applications = [AllowedApplication(name: "A", bundleID: "test.a", path: "/Applications/A.app")]
        try config.add(name: " Second ", copyCurrent: true)
        let secondID = config.activeProfileID
        XCTAssertNotEqual(firstID, secondID)
        XCTAssertEqual(config.profile.name, "Second")
        XCTAssertFalse(config.profile.rememberPassword, "A copied connection must not inherit another connection's Keychain reference")
        config.profile.server = "second.example"; config.profile.username = "second"
        config.applications = [AllowedApplication(name: "B", bundleID: "test.b", path: "/Applications/B.app")]
        try config.select(firstID)
        XCTAssertEqual(config.profile.server, "first.example")
        XCTAssertEqual(config.profile.username, "first")
        XCTAssertTrue(config.profile.rememberPassword)
        XCTAssertEqual(config.applications.map(\.name), ["A"])
        try config.select(secondID)
        let restored = try JSONDecoder().decode(Configuration.self, from: JSONEncoder().encode(config))
        XCTAssertEqual(restored.activeProfileID, secondID)
        XCTAssertEqual(restored.profile.server, "second.example")
        XCTAssertEqual(restored.applications.map(\.name), ["B"])
        try config.remove(secondID)
        XCTAssertEqual(config.activeProfileID, firstID)
        XCTAssertThrowsError(try config.remove(firstID))
        try config.add(name: "Empty", copyCurrent: false)
        XCTAssertEqual(config.profile.server, "")
        XCTAssertTrue(config.applications.isEmpty)
    }
    func testLegacyMigrationPreservesProfileIdentityAndOriginalFile() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: dir) }
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let file = ConfigurationFile(url: dir.appendingPathComponent("configuration.json"))
        let id = UUID()
        let legacy = Data("""
        {"schemaVersion":1,"profile":{"id":"\(id)","name":"Legacy","server":"vpn.example","username":"test","group":"work","rememberPassword":true},"applications":[{"name":"A","bundleID":"test.a","path":"/Applications/A.app"}]}
        """.utf8)
        try legacy.write(to: file.url)
        var migrated = try file.load()
        XCTAssertEqual(migrated.schemaVersion, 7)
        XCTAssertEqual(migrated.activeProfileID, id)
        XCTAssertEqual(migrated.profile.id, id)
        XCTAssertTrue(migrated.profile.rememberPassword)
        XCTAssertEqual(migrated.applications.map(\.name), ["A"])
        try file.save(migrated)
        let backup = dir.appendingPathComponent("configuration.v1.backup.json")
        XCTAssertEqual(try Data(contentsOf: backup), legacy)
        XCTAssertEqual((try FileManager.default.attributesOfItem(atPath: backup.path)[.posixPermissions] as? NSNumber)?.intValue, 0o600)
        try migrated.add(name: "New", copyCurrent: false)
        try file.save(migrated)
        XCTAssertEqual(try file.load(), migrated)
        XCTAssertEqual(try Data(contentsOf: backup), legacy)
    }
    func testRejectsAmbiguousOrMissingProfileIdentity() throws {
        let config = Configuration()
        let encoded = try JSONEncoder().encode(config)
        let original = try XCTUnwrap(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
        let connections = try XCTUnwrap(original["connections"] as? [[String: Any]])
        for broken in [[], connections + connections] {
            var object = original; object["connections"] = broken
            XCTAssertThrowsError(try JSONDecoder().decode(Configuration.self, from: JSONSerialization.data(withJSONObject: object)))
        }
        var object = original; object["activeProfileID"] = UUID().uuidString
        XCTAssertThrowsError(try JSONDecoder().decode(Configuration.self, from: JSONSerialization.data(withJSONObject: object)))
    }
    func testFramingIgnoresUnstructuredLogsAndHandlesSplitReads() {
        var decoder = EventDecoder()
        XCTAssertTrue(decoder.feed(Data("{\"event\":\"rea".utf8)).isEmpty)
        let events = decoder.feed(Data("dy\",\"port\":1234}\nraw password log\n{\"event\":\"stopped\"}\n".utf8))
        XCTAssertEqual(events.map(\.event), ["ready", "stopped"])
        XCTAssertEqual(events.first?.port, 1234)
    }
    func testModesAndBothListsPersistIndependentlyPerConnection() throws {
        var config = Configuration()
        let firstID = config.activeProfileID
        let a = AllowedApplication(name: "VPN", bundleID: "test.vpn", path: "/Applications/VPN.app")
        let b = AllowedApplication(name: "Direct", bundleID: "test.direct", path: "/Applications/Direct.app")
        config.applications = [a]; config.excludedApplications = [b]
        XCTAssertEqual(config.routingApplications, [a])
        config.routingMode = .global
        XCTAssertEqual(config.routingApplications, [b])
        try config.add(name: "Company", copyCurrent: true)
        config.routingMode = .whitelist; config.excludedApplications = []
        try config.select(firstID)
        XCTAssertEqual(config.routingMode, .global)
        XCTAssertEqual(config.applications, [a]); XCTAssertEqual(config.excludedApplications, [b])
        let restored = try JSONDecoder().decode(Configuration.self, from: JSONEncoder().encode(config))
        XCTAssertEqual(restored, config)
        try config.add(name: "New", copyCurrent: false)
        XCTAssertEqual(config.routingMode, .whitelist)
        XCTAssertTrue(config.excludedApplications.isEmpty)
    }
    func testVersionTwoMigrationKeepsIdentityAndDefaultsToWhitelist() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let file = ConfigurationFile(url: directory.appendingPathComponent("configuration.json"))
        var object = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(Configuration())) as? [String: Any])
        object["schemaVersion"] = 2
        var items = try XCTUnwrap(object["connections"] as? [[String: Any]])
        items[0].removeValue(forKey: "routingMode"); items[0].removeValue(forKey: "excludedApplications")
        object["connections"] = items
        let original = try JSONSerialization.data(withJSONObject: object)
        try original.write(to: file.url)
        let restored = try file.load()
        XCTAssertEqual(restored.schemaVersion, 7); XCTAssertEqual(restored.routingMode, .whitelist)
        XCTAssertTrue(restored.excludedApplications.isEmpty)
        XCTAssertEqual(restored.activeProfileID.uuidString.lowercased(), (object["activeProfileID"] as? String)?.lowercased())
        try file.save(restored)
        XCTAssertEqual(try Data(contentsOf: directory.appendingPathComponent("configuration.v2.backup.json")), original)
    }
}

final class SubscriptionAndDomainTests: XCTestCase {
    func testVersionThreeMigrationPreservesIdentityAndLists() throws {
        var original = Configuration()
        original.profile.name = "Existing"
        original.applications = [AllowedApplication(name: "Test", bundleID: "test", path: "/Applications/Test.app")]
        var object = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(original)) as? [String: Any])
        object["schemaVersion"] = 3
        var lines = try XCTUnwrap(object["connections"] as? [[String: Any]])
        lines[0].removeValue(forKey: "directDomains"); lines[0].removeValue(forKey: "subscription"); object["connections"] = lines
        let migrated = try JSONDecoder().decode(Configuration.self, from: JSONSerialization.data(withJSONObject: object))
        XCTAssertEqual(migrated.activeProfileID, original.activeProfileID)
        XCTAssertEqual(migrated.applications, original.applications)
        XCTAssertEqual(migrated.schemaVersion, 7)
        XCTAssertTrue(migrated.directDomains.isEmpty)
        XCTAssertNil(migrated.activeConnection.subscription)
    }
    func testDomainsAreNormalizedAndScopedToLine() throws {
        XCTAssertEqual(try DomainRule(" *.Example.com. ").domain, "example.com")
        for invalid in ["https://example.com", "example.com/a", "127.0.0.1", "a..com", "a.com,DIRECT", "-a.com"] {
            XCTAssertThrowsError(try DomainRule(invalid))
        }
        var c = Configuration(); c.directDomains = [try DomainRule("example.com")]
        try c.add(name: "Other", copyCurrent: false)
        XCTAssertTrue(c.directDomains.isEmpty)
        try c.select(c.connections[0].id)
        XCTAssertEqual(c.directDomains.count, 1)
        let roundTrip = try JSONDecoder().decode(Configuration.self, from: JSONEncoder().encode(c))
        XCTAssertEqual(roundTrip, c)
    }
    func testSubscriptionEditorDoesNotRequireAnyConnectCredentials() throws {
        var c = Configuration()
        c.connections[0].subscription = SubscriptionSelection(subscriptionID: UUID(), nodeName: "Node")
        var draft = ConnectionDraft(editing: c.connections[0])
        draft.connection.profile.name = "Renamed"
        try c.save(draft)
        XCTAssertEqual(c.profile.name, "Renamed")
        XCTAssertEqual(c.activeConnection.subscription?.nodeName, "Node")
    }
}

final class DirectExceptionsTests: XCTestCase {
    func testDomainsApplyInBothModesAndDraftSaveIsPerLine() throws {
        var c = Configuration()
        c.profile.server = "vpn.example.com"; c.profile.username = "qa"
        let app = AllowedApplication(name: "QA", bundleID: "qa", path: "/Applications/QA.app")
        c.applications = [app]
        let original = c
        var draft = ConnectionDraft(editing: c.activeConnection)
        draft.connection.directDomains = [try DomainRule("*.Example.com.")]
        XCTAssertEqual(c, original)
        try c.save(draft)
        XCTAssertEqual(c.routingMode, .whitelist)
        XCTAssertEqual(c.routingApplications, [app])
        XCTAssertEqual(c.effectiveDirectDomains, [try DomainRule("example.com")])
        let first = c.activeProfileID
        try c.add(name: "Second", copyCurrent: false)
        XCTAssertTrue(c.directDomains.isEmpty)
        try c.select(first)
        c.routingMode = .global
        XCTAssertEqual(c.effectiveDirectDomains.count, 1)
        XCTAssertTrue(c.routingApplications.isEmpty)
        c.routingMode = .whitelist
        XCTAssertEqual(c.effectiveDirectDomains.count, 1)
    }
    func testDirectAppsOverrideWhitelistAndPersistAcrossModes() throws {
        var c = Configuration()
        let a = AllowedApplication(name: "VPN", bundleID: "qa.vpn", path: "/Applications/VPN.app")
        let b = AllowedApplication(name: "Direct", bundleID: "qa.direct", path: "/Applications/Direct.app")
        c.applications = [a,b]; c.excludedApplications = [b]
        XCTAssertEqual(c.routingApplications, [a])
        XCTAssertEqual(c.activeConnection.conflictingApplications, [b])
        c.routingMode = .global
        XCTAssertEqual(c.routingApplications, [b])
        c = try JSONDecoder().decode(Configuration.self, from: JSONEncoder().encode(c))
        c.routingMode = .whitelist
        XCTAssertEqual(c.applications, [a,b])
        XCTAssertEqual(c.routingApplications, [a])
        c.excludedApplications = [a,b]
        XCTAssertTrue(c.routingApplications.isEmpty)
    }
    func testVersionFourPreservesModeAndDomainExceptions() throws {
        var c = Configuration(); c.directDomains = [try DomainRule("example.com")]
        c.schemaVersion = 4
        let restored = try JSONDecoder().decode(Configuration.self, from: JSONEncoder().encode(c))
        XCTAssertEqual(restored.schemaVersion, 7)
        XCTAssertEqual(restored.activeProfileID, c.activeProfileID)
        XCTAssertEqual(restored.routingMode, .whitelist)
        XCTAssertEqual(restored.effectiveDirectDomains, c.directDomains)
    }
    func testConcurrentDomainEditsAreRejected() throws {
        var c = Configuration(); c.profile.server = "vpn.example.com"; c.profile.username = "qa"
        let draft = ConnectionDraft(editing: c.activeConnection)
        c.directDomains = [try DomainRule("example.com")]
        XCTAssertThrowsError(try c.save(draft))
    }
}

final class NodeLatencyResultTests: XCTestCase {
    func testOnlyExpectedHTTPSResponseCountsAsSuccess() {
        XCTAssertEqual(NodeLatencyResult.parse(curlExitCode: 0, output: "204 0.123456").milliseconds, 123)
        XCTAssertEqual(NodeLatencyResult.parse(curlExitCode: 0, output: "204 0.000000").milliseconds, 1)
        for value in ["200 0.12", "403 0.1", "000 0.0", "204 nan", "204 inf", "204 -1", "204 1 extra", ""] {
            XCTAssertEqual(NodeLatencyResult.parse(curlExitCode: 0, output: value).outcome, .unavailable)
        }
        XCTAssertEqual(NodeLatencyResult.parse(curlExitCode: 60, output: "204 0.2").outcome, .unavailable)
        XCTAssertEqual(NodeLatencyResult.parse(curlExitCode: 28, output: "000 8.0").outcome, .timeout)
    }
    func testStoredResultsPreserveTimeAndOutcome() throws {
        let result = NodeLatencyResult(outcome: .success, milliseconds: 123, checkedAt: Date(timeIntervalSince1970: 123456))
        XCTAssertEqual(try JSONDecoder().decode(NodeLatencyResult.self, from: JSONEncoder().encode(result)), result)
        XCTAssertEqual(result.label, "123 ms")
        XCTAssertEqual(NodeLatencyResult(outcome: .cancelled).label, "已取消")
    }
}
