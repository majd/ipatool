// swift-tools-version:5.5

import PackageDescription

let package = Package(
    name: "CLI",
    platforms: [.iOS(.v13), .macOS(.v10_15)],
    products: [
        .executable(name: "ipatool", targets: ["IPATool"])
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-argument-parser", from: "0.4.3"),
        .package(name: "Networking", path: "../Networking"),
        .package(name: "StoreAPI", path: "../StoreAPI")
    ],
    targets: [
        .executableTarget(name: "IPATool", dependencies: [
            .product(name: "ArgumentParser", package: "swift-argument-parser"),
            .product(name: "Networking", package: "Networking"),
            .product(name: "StoreAPI", package: "StoreAPI")
        ]),
    ]
)
