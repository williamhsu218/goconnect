// swift-tools-version: 5.10
import PackageDescription
import Foundation

let versionURL = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("VERSION.json")
let version = try JSONSerialization.jsonObject(with: Data(contentsOf: versionURL)) as! [String: String]

let package = Package(
    name: "GoConnect",
    platforms: [.macOS(version["deploymentTarget"]!)],
    products: [
        .executable(name: "GoConnect", targets: ["GoConnect"]),
        .library(name: "GoConnectCore", targets: ["GoConnectCore"])
    ],
    targets: [
        .target(name: "GoConnectCore"),
        .executableTarget(name: "GoConnect", dependencies: ["GoConnectCore"],
                          linkerSettings: [.linkedFramework("SwiftUI"), .linkedFramework("Security")]),
        .testTarget(name: "GoConnectCoreTests", dependencies: ["GoConnectCore"]),
        .testTarget(name: "GoConnectAppTests", dependencies: ["GoConnect", "GoConnectCore"])
    ]
)
