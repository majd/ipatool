//
//  ConsoleLogger.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

final class ConsoleLogger: Logging {
    private let level: LogLevel
    
    init(level: LogLevel) {
        self.level = level
    }
    
    func log(_ message: String, level: LogLevel) {
        guard level <= self.level else { return }
        print(compile(message, level: level))
    }
    
    func log(_ message: String, prefix: String, level: LogLevel) {
        guard level <= self.level else { return }
        print("\(prefix)\(compile(message, level: level))")
    }

    func compile(_ message: String, level: LogLevel) -> String {
        return "==> \(level.prefix)\(message)".trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
