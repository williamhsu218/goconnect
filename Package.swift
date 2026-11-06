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
    dependencies: [
        .package(url: "https://github.com/sparkle-project/Sparkle", exact: "2.10.1")
    ],
    targets: [
        .target(name: "GoConnectCore"),
        .executableTarget(name: "GoConnect", dependencies: ["GoConnectCore", .product(name: "Sparkle", package: "Sparkle")],
                          linkerSettings: [.linkedFramework("SwiftUI"), .linkedFramework("Security"), .linkedLibrary("sqlite3"), .unsafeFlags(["-Xlinker", "-rpath", "-Xlinker", "@executable_path/../Frameworks"])]),
        .testTarget(name: "GoConnectCoreTests", dependencies: ["GoConnectCore"]),
        .testTarget(name: "GoConnectAppTests", dependencies: ["GoConnect", "GoConnectCore"])
    ]
)
