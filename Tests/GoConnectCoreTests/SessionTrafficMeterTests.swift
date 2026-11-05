import XCTest
@testable import GoConnectCore

final class SessionTrafficMeterTests: XCTestCase {
    func testIrregularSamplesIdleAndCounterReset() {
        var meter = SessionTrafficMeter()
        XCTAssertEqual(meter.sample(received:100,sent:50,time:10).download,0)
        let active = meter.sample(received:2148,sent:562,time:12)
        XCTAssertEqual(active.download,1024); XCTAssertEqual(active.upload,256)
        let idle = meter.sample(received:2148,sent:562,time:13)
        XCTAssertEqual(idle.download,0); XCTAssertEqual(idle.upload,0)
        let reset = meter.sample(received:2,sent:1,time:14)
        XCTAssertEqual(reset.download,0); XCTAssertEqual(reset.upload,0)
    }
    func testUnavailableGapAndNewSessionCannotProduceASpike() {
        var meter = SessionTrafficMeter()
        _ = meter.sample(received:0,sent:0,time:10)
        XCTAssertEqual(meter.sample(received:1_000_000,sent:100,time:20).download,0)
        meter.reset()
        XCTAssertEqual(meter.sample(received:2_000_000,sent:200,time:21).download,0)
        XCTAssertEqual(meter.sample(received:2_001_000,sent:300,time:22).download,1000)
    }
}
