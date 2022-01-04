//
//  AsyncSupport.swift
//  IPATool
//
//  Created by Majd Alfhaily on 04.01.22.
//

import Foundation
import ArgumentParser

protocol AsyncParsableCommand: ParsableCommand {
    mutating func run() async throws
}

extension AsyncParsableCommand {
  mutating func run() throws {
    throw CleanExit.helpRequest(self)
  }
}

protocol AsyncMain {
  associatedtype Command: ParsableCommand
}

extension AsyncMain {
  static func main() async {
    do {
      var command = try Command.parseAsRoot()
      if var command = command as? AsyncParsableCommand {
        try await command.run()
      } else {
        try command.run()
      }
    } catch {
      Command.exit(withError: error)
    }
  }
}
