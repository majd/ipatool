//
//  Download.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import ArgumentParser

struct Download: ParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(abstract: "Download (encrypted) iOS app packages from the App Store.")
    }

    @Option(name: [.short, .long], help: "The bundle identifier of the target iOS app.")
    private var bundleIdentifier: String

    @Option(name: [.short, .long], help: "The email address for the Apple ID.")
    private var email: String?

    @Option(name: [.short, .long], help: "The password for the Apple ID.")
    private var password: String?

    @Option(name: [.short, .customLong("output")], help: "The path for saving the iOS app package.")
    private var outputPath: String
}

extension Download {
    func run() throws {
        
    }
}
