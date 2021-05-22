//
//  Search.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import ArgumentParser
import Foundation

struct Search: ParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(abstract: "Search for iOS apps available on the App Store.")
    }

    @Option
    private var logLevel: LogLevel = .info

    @Option
    private var limit: Int = 5

    @Argument(help: "The term to search for.")
    var term: String
}

extension Search {
    func run() throws {
        let logger = ConsoleLogger(level: logLevel)
        
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(urlSession: URLSession.shared)

        logger.log("Creating iTunes client...", level: .debug)
        let itunesClient = iTunesClient(httpClient: httpClient)
        
        do {
            logger.log("Searching for '\(term)'...", level: .info)
            let results = try itunesClient.search(term: term, limit: limit)
            
            guard !results.isEmpty else {
                logger.log("No results found.", level: .error)
                _exit(1)
            }
            
            logger.log("Found \(results.count) results:\n\(results.enumerated().map({ "\($0 + 1). \($1.name): \($1.bundleIdentifier) (\($1.version))." }).joined(separator: "\n"))", level: .info)
        } catch {
            logger.log("\(error)", level: .debug)
            logger.log("An unknown error has occurred.", level: .error)
            _exit(1)
        }
    }
}
