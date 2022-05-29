//
//  Search.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import ArgumentParser
import Foundation
import Networking
import StoreAPI

struct Search: ParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(abstract: "Search for iOS apps available on the App Store.")
    }

    @Argument(help: "The term to search for.")
    private var term: String

    @Option(name: [.short, .long], help: "The maximum amount of search results to retrieve.")
    private var limit: Int = 5

    @Option(
        name: [.customShort("c"), .customLong("country")],
        help: "The two-letter (ISO 3166-1 alpha-2) country code for the iTunes Store."
    )
    private var countryCode: String = "US"

    @Option(name: [.short, .long], help: "The device family to limit the search query to.")
    private var deviceFamily: DeviceFamily = .phone

    @Option(name: [.long], help: "The log level.")
    private var logLevel: LogLevel = .info
    
    lazy var logger = ConsoleLogger(level: logLevel)
}

extension Search {
    mutating func results(with term: String) -> [iTunesResponse.Result] {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(session: URLSession.shared)

        logger.log("Creating iTunes client...", level: .debug)
        let itunesClient = iTunesClient(httpClient: httpClient)

        logger.log("Searching for '\(term)' using the '\(countryCode)' store front...", level: .info)

        do {
            let results = try itunesClient.search(
                term: term,
                limit: limit,
                countryCode: countryCode,
                deviceFamily: deviceFamily
            )

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
        let results = results(with: term)

        // Compile output
        let output = results
            .enumerated()
            .map({ "\($0 + 1). \($1.name): \($1.bundleIdentifier) (\($1.version))." })
            .joined(separator: "\n")

        logger.log("Found \(results.count) \(results.count == 1 ? "result" : "results"):\n\(output)", level: .info)
    }
}
