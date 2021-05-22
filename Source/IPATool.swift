//
//  IPATool.swift
//  ipatool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import ArgumentParser

struct IPATool: ParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(commandName: "ipatool",
                     abstract: "A cli tool for interacting with Apple ipa files.",
                     version: "0.9.0",
                     subcommands: [Download.self],
                     defaultSubcommand: Download.self)
    }
}
