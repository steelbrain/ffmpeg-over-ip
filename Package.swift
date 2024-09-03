// swift-tools-version: 5.10
// The swift-tools-version declares the minimum version of Swift required to build this package.

import PackageDescription

let package = Package(
    name: "ffmpeg-over-ip",
    targets: [
        .target(name: "common"),
        .executableTarget(name: "server", dependencies: ["common"]),
        .executableTarget(name: "client", dependencies: ["common"])
    ]
)
