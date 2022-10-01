// swift-tools-version:5.6

import PackageDescription

let package = Package(
    name: "IPATool",
    platforms: [.macOS(.v10_11)],
    products: [
        .executable(name: "ipatool", targets: ["CLI"]),
        .library(name: "StoreAPI", targets: ["StoreAPI"]),
        .library(name: "Networking", targets: ["Networking"]),
        .library(name: "Persistence", targets: ["Persistence"])
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-argument-parser", revision: "1.1.4"),
        .package(url: "https://github.com/weichsel/ZIPFoundation", revision: "0.9.15"),
        .package(url: "https://github.com/kishikawakatsumi/KeychainAccess", revision: "v4.2.2")
    ],
    targets: [
        // IPATool CLI
        .executableTarget(
            name: "CLI",
            dependencies: [
                .product(name: "ArgumentParser", package: "swift-argument-parser"),
                .byName(name: "Networking"),
                .byName(name: "StoreAPI"),
                .byName(name: "Persistence")
            ],
            plugins: ["SwiftLint"]
        ),

        // StoreAPI
        .target(
            name: "StoreAPI",
            dependencies: [
                .product(name: "ArgumentParser", package: "swift-argument-parser"),
                .product(name: "ZIPFoundation", package: "ZIPFoundation"),
                .byName(name: "Networking"),
            ],
            plugins: ["SwiftLint"]
        ),

        // Networking
        .target(
            name: "Networking",
            plugins: ["SwiftLint"]
        ),
        .testTarget(
            name: "NetworkingTests",
            dependencies: ["Networking"],
            plugins: ["SwiftLint"]
        ),

        // Persistence
        .target(
            name: "Persistence",
            dependencies: [
                .byName(name: "KeychainAccess")
            ],
            plugins: ["SwiftLint"]
        ),

        // SwiftLint
        .plugin(name: "SwiftLint", capability: .buildTool())
    ]
)
