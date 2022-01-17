// swift-tools-version:5.5

import PackageDescription

let package = Package(
    name: "IPATool",
    platforms: [.iOS(.v13), .macOS(.v10_15)],
    products: [
        .executable(name: "ipatool", targets: ["CLI"]),
        .library(name: "StoreAPI", targets: ["StoreAPI"]),
        .library(name: "Networking", targets: ["Networking"])
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-argument-parser", from: "0.4.3"),
        .package(url: "https://github.com/weichsel/ZIPFoundation.git", from: "0.9.12")
    ],
    targets: [
        .executableTarget(name: "CLI", dependencies: [
            .product(name: "ArgumentParser", package: "swift-argument-parser"),
            .byName(name: "Networking"),
            .byName(name: "StoreAPI")
        ]),
        .target(
            name: "StoreAPI",
            dependencies: [
                .product(name: "ArgumentParser", package: "swift-argument-parser"),
                .product(name: "ZIPFoundation", package: "ZIPFoundation"),
                .byName(name: "Networking"),
            ]
        ),
        .target(name: "Networking", dependencies: []),
        .testTarget(name: "NetworkingTests", dependencies: ["Networking"])
    ]
)
