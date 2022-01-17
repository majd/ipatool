// swift-tools-version:5.5

import PackageDescription

let package = Package(
    name: "StoreAPI",
    platforms: [.iOS(.v13), .macOS(.v10_15)],
    products: [
        .library(
            name: "StoreAPI",
            targets: ["StoreAPI"]
        ),
    ],
    dependencies: [
        .package(name: "Networking", path: "../Networking"),
        .package(url: "https://github.com/apple/swift-argument-parser", from: "0.4.3"),
        .package(url: "https://github.com/weichsel/ZIPFoundation.git", from: "0.9.12")
    ],
    targets: [
        .target(
            name: "StoreAPI",
            dependencies: [
                .product(name: "ArgumentParser", package: "swift-argument-parser"),
                .product(name: "Networking", package: "Networking"),
                .product(name: "ZIPFoundation", package: "ZIPFoundation")
            ]
        ),
        .testTarget(
            name: "StoreAPITests",
            dependencies: ["StoreAPI"]
        ),
    ]
)
