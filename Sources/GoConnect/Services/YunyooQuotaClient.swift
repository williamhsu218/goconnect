import Foundation
import WebKit
import GoConnectCore

struct YunyooRelaySession {
    let body: Data
    let snapshot: YunyooTrafficSnapshot
}

/// A normal, persistent WebKit login session. No Chrome cookies, provider API
/// keys, passwords, or raw product responses are copied into GoConnect JSON.
@MainActor
final class YunyooQuotaClient: NSObject, WKNavigationDelegate {
    private var instance: WKWebView?
    private var navigationFinished = false
    private var navigationFailed = false

    var webView: WKWebView {
        if let instance { return instance }
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .default()
        let view = WKWebView(frame: .zero, configuration: configuration)
        view.navigationDelegate = self
        instance = view
        return view
    }

    func prepare(forceReload: Bool = false) async throws {
        let view = webView
        if !forceReload, navigationFinished, view.url?.scheme == "https", view.url?.host == "yunyoo.cc", !view.isLoading { return }
        if forceReload || view.url == nil || navigationFailed {
            navigationFinished = false; navigationFailed = false
            view.load(URLRequest(url: URL(string: "https://yunyoo.cc/clientarea")!,
                                 cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 20))
        }
        for _ in 0..<200 {
            try Task.checkCancellation()
            if navigationFailed { throw YunyooTrafficError.unavailable }
            if navigationFinished, !view.isLoading { return }
            try await Task.sleep(for: .milliseconds(100))
        }
        throw YunyooTrafficError.unavailable
    }

    static let quotaScript = """
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10000);
    try {
        const response = await fetch('/host/dedicatedserver?host_id=' + encodeURIComponent(serviceID), {
            method: 'GET', credentials: 'same-origin', mode: 'same-origin',
            cache: 'no-store', redirect: 'manual', signal: controller.signal,
            headers: {'Accept': 'application/json'}
        });
        if (response.type === 'opaqueredirect' || response.status === 401 || response.status === 403) return JSON.stringify({status: 401});
        if (!response.ok) return JSON.stringify({status: 503});
        const text = await response.text();
        if (text.length > 2097152) return JSON.stringify({status: 503});
        let data;
        try { data = JSON.parse(text); } catch { return JSON.stringify({status: 405}); }
        if (data.status !== 200) return JSON.stringify({status: data.status});
        const host = data.data && data.data.host_data;
        return JSON.stringify({status: data.status, data: host ? {host_data: {
            bwlimit: host.bwlimit, bwusage: host.bwusage, dedicatedip: host.dedicatedip
        }} : null});
    } finally { clearTimeout(timeout); }
    """

    func fetch(serviceID: String, server: String) async throws -> YunyooTrafficSnapshot {
        guard YunyooTrafficSnapshot.validServiceID(serviceID) else { throw YunyooTrafficError.invalidResponse }
        try await prepare()
        guard webView.url?.scheme == "https", webView.url?.host == "yunyoo.cc" else { throw YunyooTrafficError.loginRequired }
        // The endpoint also returns unrelated product details. Only the billing
        // fields and public server address cross the WebKit/Swift boundary.
        let value: Any?
        do {
            value = try await webView.callAsyncJavaScript(Self.quotaScript, arguments: ["serviceID": serviceID],
                                                         in: nil, contentWorld: .defaultClient)
        } catch { throw YunyooTrafficError.unavailable }
        try Task.checkCancellation()
        guard let json = value as? String, let data = json.data(using: .utf8) else { throw YunyooTrafficError.invalidResponse }
        return try YunyooTrafficSnapshot.decode(data, serviceID: serviceID, expectedServer: server)
    }

    func relaySession(serviceID: String, server: String) async throws -> YunyooRelaySession {
        let snapshot = try await fetch(serviceID: serviceID, server: server)
        let cookies: [HTTPCookie] = await withCheckedContinuation { continuation in
            webView.configuration.websiteDataStore.httpCookieStore.getAllCookies { continuation.resume(returning: $0) }
        }
        let scoped = cookies.filter {
            ($0.domain == "yunyoo.cc" || $0.domain == ".yunyoo.cc") && ($0.expiresDate == nil || $0.expiresDate! > Date())
        }
        guard !scoped.isEmpty, let agent = try await webView.evaluateJavaScript("navigator.userAgent") as? String else { throw YunyooTrafficError.loginRequired }
        let cookie = HTTPCookie.requestHeaderFields(with: scoped)["Cookie"] ?? ""
        guard !cookie.isEmpty else { throw YunyooTrafficError.loginRequired }
        return YunyooRelaySession(body: try JSONEncoder().encode(["cookie":cookie,"user_agent":agent]), snapshot: snapshot)
    }

    func webView(_ webView: WKWebView, didStartProvisionalNavigation navigation: WKNavigation!) {
        navigationFinished = false; navigationFailed = false
    }
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) { navigationFinished = true }
    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) { navigationFailed = true }
    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) { navigationFailed = true }
    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        // Allow normal subresources/captcha frames. Keep the main login window
        // on the official HTTPS origin; never override TLS trust decisions.
        if navigationAction.targetFrame?.isMainFrame != false {
            guard let url = navigationAction.request.url, url.scheme == "https", url.host == "yunyoo.cc" else {
                decisionHandler(.cancel); return
            }
        }
        decisionHandler(.allow)
    }
}
