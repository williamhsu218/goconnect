import XCTest
@testable import GoConnectCore

final class ProxyConnectivityResultTests: XCTestCase {
    func testRequiresSuccessfulTransportAndExpectedResponse() {
        XCTAssertTrue(ProxyConnectivityResult(exitCode: 0, output: "204 0.345").succeeded)
        XCTAssertEqual(ProxyConnectivityResult(exitCode: 0, output: "204 0.345").milliseconds, 345)
        for response in ["200 0.1", "302 0.1", "000 0", "204 nan", "204 inf", "204 -1", "204 99999999999999", "204 0.1 extra", ""] {
            XCTAssertFalse(ProxyConnectivityResult(exitCode: 0, output: response).succeeded)
        }
        XCTAssertFalse(ProxyConnectivityResult(exitCode: 60, output: "204 0.1").succeeded)
    }
    func testExplicitProxyOverridesAmbientConfiguration() {
        let args = ProxyConnectivityResult.arguments(port: 12345)
        XCTAssertEqual(args.first, "--disable")
        XCTAssertTrue(args.contains("http://127.0.0.1:12345"))
        XCTAssertEqual(args[args.firstIndex(of: "--noproxy")! + 1], "")
        XCTAssertFalse(args.contains("--insecure"))
        XCTAssertTrue(args.contains("--max-time"))
    }
}
