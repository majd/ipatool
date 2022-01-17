//
//  LogLevel.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation
import ArgumentParser

enum LogLevel: String, ExpressibleByArgument {
    case error
    case warning
    case info
    case debug
}

extension LogLevel {
    var prefix: String {
        switch self {
        case .error:
            return "❌\t[Error] "
        case .warning:
            return "⚠️\t[Warning] "
        case .info:
            return "ℹ️\t[Info] "
        case .debug:
            return "🛠\t[Debug] "
        }
    }
    
    var priority: Int {
        switch self {
        case .error:
            return 0
        case .warning:
            return 1
        case .info:
            return 2
        case .debug:
            return 3
        }
    }
}

extension LogLevel: Comparable {
    static func < (lhs: LogLevel, rhs: LogLevel) -> Bool {
        return lhs.priority < rhs.priority
    }
}
