import XCTest
@testable import GoConnectCore

final class ApplicationLaunchReadinessTests: XCTestCase {
    func testDetectsAllOrdinaryInstancesIncludingHelpersWithoutMatchingOtherUsersOrBundles() {
        let a = AllowedApplication(name: "A", bundleID: "test.a", path: "/Applications/A.app")
        let b = AllowedApplication(name: "B", bundleID: "test.b", path: "/Applications/B App.app")
        let c = AllowedApplication(name: "C", bundleID: "test.c", path: "/Applications/C.app")
        let text = """
          501 20 100 /Applications/A.app/Contents/MacOS/A
          501 20 101 /Applications/A.app/Contents/Frameworks/Helper
          501 20 102 /Applications/B App.app/Contents/MacOS/B App
          502 20 103 /Applications/C.app/Contents/MacOS/C
          501 20 104 /Applications/C.app.other/Contents/MacOS/C
        """
        let result = ApplicationLaunchReadiness.requiringRestart([a, b, c], uid: 501, processes: ApplicationLaunchReadiness.parseProcesses(text), groups: [:])
        XCTAssertEqual(result.map(\.name), ["A", "B"])
    }

    func testAlreadyTaggedAppDoesNotNeedRestartButMixedInstancesDo() {
        let app = AllowedApplication(name: "A", bundleID: "test.a", path: "/Applications/A.app")
        let groups: [String: UInt32] = [ApplicationLaunchReadiness.groupName(uid: 501, path: app.path): 61000]
        let tagged = "501 61000 123 /Applications/A.app/Contents/MacOS/A"
        XCTAssertTrue(ApplicationLaunchReadiness.requiringRestart([app], uid: 501, processes: ApplicationLaunchReadiness.parseProcesses(tagged), groups: groups).isEmpty)
        let mixed = tagged + "\n501 20 124 /Applications/A.app/Contents/Frameworks/Helper"
        XCTAssertEqual(ApplicationLaunchReadiness.requiringRestart([app], uid: 501, processes: ApplicationLaunchReadiness.parseProcesses(mixed), groups: groups).count, 1)
        XCTAssertTrue(ApplicationLaunchReadiness.requiringRestart([app], uid: 501, processes: [], groups: groups).isEmpty)
    }
}
