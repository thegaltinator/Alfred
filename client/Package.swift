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
            dependencies: ["Bridge", "Heartbeat", "TTS", "AudioIO"],
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
            dependencies: ["Memory", "TTS"],
            path: "Bridge"
        ),
        .target(
            name: "Memory",
            dependencies: ["CLlama", "CSqliteVec"],
            path: "Memory",
            exclude: ["CLlama", "CSqliteVec"],
            linkerSettings: [
                .linkedFramework("CoreML", .when(platforms: [.macOS])),
                .linkedFramework("Accelerate", .when(platforms: [.macOS])),
                .linkedLibrary("sqlite3", .when(platforms: [.macOS]))
            ]
        ),
        .target(
            name: "CSqliteVec",
            dependencies: [],
            path: "Memory/CSqliteVec",
            publicHeadersPath: ".",
            cSettings: [
                .define("SQLITE_CORE"),
                .define("SQLITE_VEC_ENABLE_NEON")
            ]
        ),
        .target(
            name: "CLlama",
            dependencies: [],
            path: "Memory/CLlama",
            publicHeadersPath: "."
        ),
        .testTarget(
            name: "MemoryTests",
            dependencies: ["Memory"],
            path: "Tests/MemoryTests"
        ),
    ]
)
