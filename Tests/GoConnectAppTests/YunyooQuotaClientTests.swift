import AppKit
import WebKit
import XCTest
@testable import GoConnect

@MainActor
final class YunyooQuotaClientTests: XCTestCase {
    private final class LoadedPage: NSObject, WKNavigationDelegate {
        let ready: XCTestExpectation
        init(_ ready: XCTestExpectation) { self.ready = ready }
        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) { ready.fulfill() }
    }

    func testLoginRedirectRequiresAuthorizationAndServerErrorsRemainRetryable() async throws {
        _ = NSApplication.shared
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        let view = WKWebView(frame: .zero, configuration: configuration)
        let ready = expectation(description: "isolated local page loaded")
        let delegate = LoadedPage(ready)
        view.navigationDelegate = delegate
        view.loadHTMLString("<html><body>Quota response fixture</body></html>", baseURL: nil)
        await fulfillment(of: [ready], timeout: 10)
        let cases: [(String, Int)] = [
            ("({type:'opaqueredirect',status:0,ok:false})", 401),
            ("({type:'basic',status:401,ok:false})", 401),
            ("({type:'basic',status:503,ok:false})", 503)
        ]
        for (response, expectedStatus) in cases {
            let script = "const fetch = async () => " + response + ";\n" + YunyooQuotaClient.quotaScript
            let raw = try await view.callAsyncJavaScript(script, arguments: ["serviceID": "123456"],
                                                         in: nil, contentWorld: .defaultClient)
            let data = try XCTUnwrap((raw as? String)?.data(using: .utf8))
            let result = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Int])
            XCTAssertEqual(result["status"], expectedStatus)
        }
        view.navigationDelegate = nil
        view.stopLoading()
    }
}
