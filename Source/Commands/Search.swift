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
    private var limit: Int = 5

    @Option(help: "The two-letter (ISO 3166-1 alpha-2) country code for the store you want to search.")
    var country: String = "US"

    @Argument(help: "The term to search for.")
    var term: String

    @Option
    private var logLevel: LogLevel = .info
    
    lazy var logger = ConsoleLogger(level: logLevel)
}

extension Search {
    mutating func results(with term: String, country: String) -> [iTunesResponse.Result] {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(urlSession: URLSession.shared)

        logger.log("Creating iTunes client...", level: .debug)
        let itunesClient = iTunesClient(httpClient: httpClient)
        
        do {
            logger.log("Searching for '\(term)' using the '\(country)' store front...", level: .info)
            let results = try itunesClient.search(term: term, limit: limit, country: country)
            
            guard !results.isEmpty else {
                logger.log("No results found.", level: .error)
                _exit(1)
            }

            return results
        } catch {
            logger.log("\(error)", level: .debug)
            logger.log("An unknown error has occurred.", level: .error)
            _exit(1)
        }
    }
    
    mutating func run() throws {
        // Search the iTunes store
        let results = results(with: term, country: country)

        // Compile output
        let output = results
            .enumerated()
            .map({ "\($0 + 1). \($1.name): \($1.bundleIdentifier) (\($1.version))." })
            .joined(separator: "\n")

        logger.log("Found \(results.count) \(results.count == 1 ? "result" : "results"):\n\(output)", level: .info)
    }
}
