import Foundation
import XCTest
@testable import GoConnect

final class ApplicationCatalogTests: XCTestCase {
    func testScanBoundsOrdinaryFoldersAndSkipsAppContents() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }
        func app(_ path: String, _ id: String) throws {
            let contents = root.appendingPathComponent(path).appendingPathComponent("Contents")
            try FileManager.default.createDirectory(at: contents.appendingPathComponent("MacOS"), withIntermediateDirectories: true)
            let info = ["CFBundleIdentifier": id, "CFBundleName": id, "CFBundlePackageType": "APPL", "CFBundleExecutable": "fixture"]
            try PropertyListSerialization.data(fromPropertyList: info, format: .xml, options: 0).write(to: contents.appendingPathComponent("Info.plist"))
            try Data().write(to: contents.appendingPathComponent("MacOS/fixture"))
        }
        try app("Top.app", "test.top")
        try app("Utilities/Suite/Visible.app", "test.visible")
        try app("Resources/Very/Large/Hidden.app", "test.deep")
        try app("Top.app/Contents/Helpers/Helper.app", "test.helper")
        XCTAssertEqual(Set(ApplicationCatalog.scan(roots: [root]).map(\.bundleID)), ["test.top", "test.visible"])
        XCTAssertEqual(ApplicationCatalog.scan(roots: [root], maximumDepth: 1).map(\.bundleID), ["test.top"])
    }
}
