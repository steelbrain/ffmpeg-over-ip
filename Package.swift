// swift-tools-version: 5.10
// The swift-tools-version declares the minimum version of Swift required to build this package.

import PackageDescription

let package = Package(
    name: "ffmpeg-over-ip",
    targets: [
        .executableTarget(name: "server"),
        .executableTarget(name: "client")
    ]
)
