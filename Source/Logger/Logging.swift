//
//  Logging.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

protocol Logging {
    func compile(_ message: String, level: LogLevel) -> String
    func log(_ message: String, prefix: String, level: LogLevel)
    func log(_ message: String, level: LogLevel)
}
