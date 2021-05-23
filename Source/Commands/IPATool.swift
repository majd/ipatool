//
//  IPATool.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import ArgumentParser

struct IPATool: ParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(commandName: "ipatool",
                     abstract: "A cli tool for interacting with Apple's ipa files.",
                     version: "1.0.0",
                     subcommands: [Download.self, Search.self])
    }
}
