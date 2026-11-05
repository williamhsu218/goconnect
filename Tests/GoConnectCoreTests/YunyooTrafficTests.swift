import XCTest
@testable import GoConnectCore

final class YunyooTrafficTests: XCTestCase {
    private func response(total: String = "1024", used: String = "3.25", ip: String = "192.0.2.10") -> Data {
        Data("{\"status\":200,\"data\":{\"host_data\":{\"bwlimit\":\(total),\"bwusage\":\(used),\"dedicatedip\":\"\(ip)\",\"password\":\"must-not-be-cached\"}}}".utf8)
    }
    func testReadsProviderGBAndMatchesPanelRoundingWithoutKeepingUnrelatedFields() throws {
        let now = Date(timeIntervalSince1970: 1_793_448_000)
        let value = try YunyooTrafficSnapshot.decode(response(), serviceID: "123456", expectedServer: "192.0.2.10", now: now)
        XCTAssertEqual(value.totalGB, 1024); XCTAssertEqual(value.usedGB, 3.25)
        XCTAssertEqual(value.displayedUsedGB, 3.3); XCTAssertEqual(value.remainingGB, 1020.7, accuracy: 0.0001)
        XCTAssertEqual(value.updatedAt, now)
        let cache = String(decoding: try JSONEncoder().encode(value), as: UTF8.self)
        XCTAssertFalse(cache.contains("password")); XCTAssertFalse(cache.contains("must-not-be-cached"))
        XCTAssertEqual(YunyooTrafficSnapshot.formatGB(value.remainingGB), "1020.7 GB")
    }
    func testStringAmountsOverageAndNoNegativeRemaining() throws {
        let value = try YunyooTrafficSnapshot.decode(response(total: "\"1024\"", used: "\"1030.25\""), serviceID: "123456", expectedServer: "192.0.2.10")
        XCTAssertEqual(value.remainingGB, 0); XCTAssertTrue(value.exceeded); XCTAssertEqual(value.usedFraction, 1)
    }
    func testAuthMissingQuotaAndWrongServerAreRejected() {
        for code in [401,403,405] {
            XCTAssertThrowsError(try YunyooTrafficSnapshot.decode(Data("{\"status\":\(code)}".utf8), serviceID: "123456", expectedServer: "192.0.2.10")) {
                XCTAssertEqual($0 as? YunyooTrafficError, .loginRequired)
            }
        }
        for data in [Data("<html>login</html>".utf8), Data("{\"status\":200}".utf8), response(total: "null"), response(total: "0"), response(used: "-1"), response(used: "\"NaN\""), response(used: "true")] {
            XCTAssertThrowsError(try YunyooTrafficSnapshot.decode(data, serviceID: "123456", expectedServer: "192.0.2.10"))
        }
        XCTAssertThrowsError(try YunyooTrafficSnapshot.decode(response(ip: "192.0.2.1"), serviceID: "123456", expectedServer: "192.0.2.10")) {
            XCTAssertEqual($0 as? YunyooTrafficError, .serverMismatch)
        }
        for id in ["", "0", "123456&action=off", "１２３４", "-1"] { XCTAssertFalse(YunyooTrafficSnapshot.validServiceID(id)) }
    }
}
