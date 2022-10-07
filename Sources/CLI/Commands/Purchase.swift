//
//  Purchase.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.03.22.
//

import ArgumentParser
import Foundation
import Networking
import StoreAPI
import Persistence

struct Purchase: ParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(abstract: "Obtain a license for the app from the App Store.")
    }

    @Option(name: [.short, .long], help: "The bundle identifier of the target iOS app.")
    private var bundleIdentifier: String

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

extension Purchase {
    private mutating func app(with bundleIdentifier: String) -> iTunesResponse.Result {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(session: URLSession.shared)

        logger.log("Creating iTunes client...", level: .debug)
        let itunesClient = iTunesClient(httpClient: httpClient)

        do {
            logger.log(
                "Querying the iTunes Store for '\(bundleIdentifier)' in country '\(countryCode)'...",
                level: .info
            )
            let app = try itunesClient.lookup(
                bundleIdentifier: bundleIdentifier,
                countryCode: countryCode,
                deviceFamily: deviceFamily
            )

            guard app.price == 0 else {
                logger.log("Only free apps are supported.", level: .error)
                _exit(1)
            }

            return app
        } catch {
            logger.log("\(error)", level: .debug)

            switch error {
            case iTunesClient.Error.appNotFound:
                logger.log("Could not find app.", level: .error)
            default:
                logger.log("An unknown error has occurred.", level: .error)
            }

            _exit(1)
        }
    }

    private mutating func purchase(app: iTunesResponse.Result, account: Account) {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(session: URLSession.shared)

        logger.log("Creating App Store client...", level: .debug)
        let storeClient = StoreClient(httpClient: httpClient)

        do {
            logger.log("Obtaining a license for '\(app.identifier)' from the App Store...", level: .info)
            try storeClient.purchase(
                identifier: "\(app.identifier)",
                directoryServicesIdentifier: account.directoryServicesIdentifier,
                passwordToken: account.passwordToken,
                countryCode: countryCode
            )
        } catch {
            logger.log("\(error)", level: .debug)

            switch error {
            case StoreClient.Error.purchaseFailed:
                logger.log("Purchase failed.", level: .error)
            case StoreClient.Error.duplicateLicense:
                logger.log("A license already exists for this item.", level: .error)
            case StoreResponse.Error.priceMismatch:
                logger.log("Price mismatch. It is only possible to obtain a license for free apps.", level: .error)
            case StoreResponse.Error.invalidCountry:
                logger.log([
                    "The country provided does not match with the account you are using.",
                    "Supply a valid country using the \"--country\" flag."
                ].joined(separator: " "), level: .error)
            case StoreResponse.Error.passwordTokenExpired, StoreResponse.Error.passwordChanged:
                logger.log("Token expired. Login again using the \"auth\" command.", level: .error)
            default:
                logger.log("An unknown error has occurred.", level: .error)
            }

            _exit(1)
        }
    }

    mutating func validate() throws {
        guard !Storefront.allCases.contains(where: { "\($0)" == countryCode }) else {
            throw ValidationError("The country code is not valid")
        }
    }

    mutating func run() throws {
        // Authenticate with the App Store
        let keychainStore = KeychainStore(service: "ipatool.service")

        guard let account: Account = try keychainStore.value(forKey: "account") else {
            logger.log("Authentication required. Run \"ipatool auth --help\" for help.", level: .error)
            _exit(1)
        }
        logger.log("Authenticated as '\(account.name)'.", level: .info)

        // Query for app
        let app: iTunesResponse.Result = app(with: bundleIdentifier)
        logger.log("Found app: \(app.name) (\(app.version)).", level: .debug)

        // Obtain a license
        purchase(app: app, account: account)
        logger.log("Obtained a license for '\(app.identifier)'.", level: .debug)
        logger.log("Done.", level: .info)
    }
}
