import XCTest
@testable import GoConnect

final class SubscriptionQuotaTests: XCTestCase {
    func testRemainingAndStaleMetadataWithoutOverflow() {
        let value = Subscription(payload:SubscriptionPayload(name:"quota",url:"https://example.invalid/sub",nodes:[],updated:1,userInfo:"upload=10; download=20; total=100; updated=123; stale=1"))
        XCTAssertEqual(value.quotaValues["updated"],123)
        XCTAssertEqual(value.quotaValues["stale"],1)
        XCTAssertTrue(value.quotaText?.contains("剩余") == true)
        var overflow=value; overflow.payload.userInfo="upload=9223372036854775807; download=30; total=100"
        XCTAssertNotNil(overflow.quotaText)
    }
    func testExpiredAuthorizationMetadataSurvivesSubscriptionParsing() {
        let value = Subscription(payload:SubscriptionPayload(name:"quota",url:"https://example.invalid/sub",nodes:[],updated:1,userInfo:"upload=0; download=100; total=1024; updated=123; stale=1; auth_required=1"))
        XCTAssertEqual(value.quotaValues["auth_required"],1)
        XCTAssertEqual(value.quotaValues["download"],100)
    }
}
