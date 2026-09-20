// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "GoConnect",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "GoConnect", targets: ["GoConnect"]),
        .library(name: "GoConnectCore", targets: ["GoConnectCore"])
    ],
    targets: [
        .target(name: "GoConnectCore"),
        .executableTarget(name: "GoConnect", dependencies: ["GoConnectCore"],
                          linkerSettings: [.linkedFramework("SwiftUI"), .linkedFramework("Security")]),
        .testTarget(name: "GoConnectCoreTests", dependencies: ["GoConnectCore"])
    ]
)
