//
//  ConsoleLogger.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation
import Darwin

final class ConsoleLogger: Logging {
    private let level: LogLevel
    
    init(level: LogLevel) {
        self.level = level
    }
    
    func log(_ message: String, level: LogLevel) {
        guard level <= self.level else { return }

        switch level {
        case .error:
            fputs("\(compile(message, level: level))\n", stderr)
        default:
            print(compile(message, level: level))
        }
    }
    
    func log(_ message: String, prefix: String, level: LogLevel) {
        guard level <= self.level else { return }
        switch level {
        case .error:
            fputs("\(prefix)\(compile(message, level: level))\n", stderr)
        default:
            print("\(prefix)\(compile(message, level: level))")
        }
    }

    func compile(_ message: String, level: LogLevel) -> String {
        return "==> \(level.prefix)\(message)".trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
