//
//  IPATool.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import ArgumentParser

@main
struct IPATool: ParsableCommand {
    static var configuration: CommandConfiguration {
        return CommandConfiguration(
            commandName: "ipatool",
            abstract: "A cli tool for interacting with Apple's ipa files.",
            version: Package.version,
            subcommands: [Auth.self, Download.self, Purchase.self, Search.self]
        )
    }
}
