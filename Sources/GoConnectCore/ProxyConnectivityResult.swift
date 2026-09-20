import Foundation

public struct ProxyConnectivityResult: Equatable, Sendable {
    public let succeeded: Bool
    public let milliseconds: Int?

    public init(exitCode: Int32, output: String) {
        let parts = output.split(whereSeparator: \.isWhitespace)
        if exitCode == 0, parts.count == 2, parts[0] == "204",
           let seconds = Double(parts[1]), seconds.isFinite, seconds >= 0, seconds <= 20 {
            succeeded = true
            milliseconds = Int((seconds * 1000).rounded())
        } else {
            succeeded = false
            milliseconds = nil
        }
    }

    public static func arguments(port: UInt16) -> [String] {
        ["--disable", "--proxy", "http://127.0.0.1:\(port)", "--noproxy", "",
         "--connect-timeout", "5", "--max-time", "10", "--silent",
         "--output", "/dev/null", "--write-out", "%{http_code} %{time_total}",
         "https://www.gstatic.com/generate_204"]
    }
}
