// swift-tools-version: 5.9
// The swift-tools-version declares the minimum version of Swift required to build this package.

import PackageDescription

let package = Package(
    name: "AlfredClient",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .executable(
            name: "AlfredClient",
            targets: ["AlfredClient"]
        ),
    ],
    targets: [
        .executableTarget(
            name: "AlfredClient",
            dependencies: ["Bridge", "Heartbeat", "TTS", "AudioIO", "Memory"],
            path: "AppKitUI"
        ),
        .target(
            name: "TTS",
            dependencies: ["AudioIO"],
            path: "TTS"
        ),
        .target(
            name: "AudioIO",
            dependencies: [],
            path: "AudioIO"
        ),
        .target(
            name: "Heartbeat",
            dependencies: [],
            path: "Heartbeat"
        ),
        .target(
            name: "Bridge",
            dependencies: ["TTS"],
            path: "Bridge"
        ),
        .target(
            name: "Memory",
            dependencies: [],
            path: "Memory",
            linkerSettings: [
                .linkedLibrary("sqlite3")
            ]
        )
    ]
)
