//
//  Download.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import ArgumentParser
import Foundation
import Networking
import StoreAPI
import Persistence

struct Download: ParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(abstract: "Download (encrypted) iOS app packages from the App Store.")
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

    @Option(name: [.short, .long], help: "The destination path of the downloaded app package.")
    private var output: String?

    @Option(name: [.long], help: "The log level.")
    private var logLevel: LogLevel = .info

    @Flag(name: .long, help: "Obtain a license for the app if needed.")
    private var purchase: Bool = false

    lazy var logger = ConsoleLogger(level: logLevel)
}

extension Download {
    private mutating func app(with bundleIdentifier: String, countryCode: String) -> iTunesResponse.Result {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(session: URLSession.shared)

        logger.log("Creating iTunes client...", level: .debug)
        let itunesClient = iTunesClient(httpClient: httpClient)

        do {
            logger.log("Querying the iTunes Store for '\(bundleIdentifier)' in country '\(countryCode)'...", level: .info)
            return try itunesClient.lookup(
                bundleIdentifier: bundleIdentifier,
                countryCode: countryCode,
                deviceFamily: deviceFamily
            )
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
        guard app.price == 0 else {
            logger.log("It is only possible to obtain a license for free apps. Purchase the app manually and run the \"download\" command again.", level: .error)
            _exit(1)
        }

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
                logger.log("The country provided does not match with the account you are using. Supply a valid country using the \"--country\" flag.", level: .error)
            case StoreResponse.Error.passwordTokenExpired:
                logger.log("Token expired. Login again using the \"auth\" command.", level: .error)
            default:
                logger.log("An unknown error has occurred.", level: .error)
            }

            _exit(1)
        }
    }

    private mutating func item(
        from app: iTunesResponse.Result,
        account: Account,
        purchaseAttempted: Bool = false
    ) -> StoreResponse.Item {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(session: URLSession.shared)

        logger.log("Creating App Store client...", level: .debug)
        let storeClient = StoreClient(httpClient: httpClient)

        do {
            logger.log("Requesting a signed copy of '\(app.identifier)' from the App Store...", level: .info)
            return try storeClient.item(
                identifier: "\(app.identifier)",
                directoryServicesIdentifier: account.directoryServicesIdentifier
            )
        } catch {
            logger.log("\(error)", level: .debug)
            
            switch error {
            case StoreClient.Error.invalidResponse:
                logger.log("Received invalid response.", level: .error)
            case StoreResponse.Error.invalidItem:
                logger.log("Received invalid store item.", level: .error)
            case StoreResponse.Error.invalidLicense:
                if !purchaseAttempted, purchase {
                    logger.log("License is missing.", level: .info)

                    purchase(app: app, account: account)
                    logger.log("Obtained a license for '\(app.identifier)'.", level: .debug)

                    return item(from: app, account: account, purchaseAttempted: true)
                } else {
                    logger.log("Your Apple ID does not have a license for this app. Use the \"purchase\" command or the \"--purchase\" to obtain a license.", level: .error)
                }
            case StoreResponse.Error.invalidCountry:
                logger.log("The country provided does not match with the account you are using. Supply a valid country using the \"--country\" flag.", level: .error)
            case StoreResponse.Error.passwordTokenExpired:
                logger.log("Token expired. Login again using the \"auth\" command.", level: .error)
            default:
                logger.log("An unknown error has occurred.", level: .error)
            }
            
            _exit(1)
        }
    }
    
    private mutating func download(item: StoreResponse.Item, to targetURL: URL) {
        logger.log("Creating download client...", level: .debug)
        let downloadClient = HTTPDownloadClient()

        do {
            logger.log("Downloading app package...", level: .info)
            try downloadClient.download(from: item.url, to: targetURL) { [logger] progress in
                logger.log("Downloading app package... [\(Int((progress * 100).rounded()))%]",
                           prefix: "\u{1B}[1A\u{1B}[K",
                           level: .info)
            }
        } catch {
            logger.log("\(error)", level: .debug)
            logger.log("An error has occurred while downloading the app package.", level: .error)
            _exit(1)
        }
    }
    
    private mutating func applyPatches(item: StoreResponse.Item, email: String, path: String) {
        logger.log("Creating signature client...", level: .debug)
        let signatureClient = SignatureClient(fileManager: .default, filePath: path)

        do {
            logger.log("Applying patches...", level: .info)
            try signatureClient.appendMetadata(item: item, email: email)
            try signatureClient.appendSignature(item: item)
        } catch {
            logger.log("\(error)", level: .debug)
            logger.log("Failed to apply patches. The ipa file will be left incomplete.", level: .error)
            _exit(1)
        }
    }
    
    private mutating func makeOutputPath(app: iTunesResponse.Result) -> String {
        let fileName: String = "/\(bundleIdentifier)_\(app.identifier)_v\(app.version)_\(Int.random(in: 100...999)).ipa"
        
        guard let output = output else {
            return FileManager.default.currentDirectoryPath.appending(fileName)
        }
        
        var isDirectory: ObjCBool = false
        FileManager.default.fileExists(atPath: output, isDirectory: &isDirectory)
        
        return isDirectory.boolValue ? output.appending(fileName) : output
    }

    mutating func validate() throws {
        guard let output = output else { return }
        
        var isDirectory: ObjCBool = false
        guard !FileManager.default.fileExists(atPath: output, isDirectory: &isDirectory) || isDirectory.boolValue else {
            logger.log("A file already exists at \(output).", level: .error)
            _exit(1)
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
        let app: iTunesResponse.Result = app(with: bundleIdentifier, countryCode: countryCode)
        logger.log("Found app: \(app.name) (\(app.version)).", level: .debug)

        // Query for store item
        let item: StoreResponse.Item = item(from: app, account: account)
        logger.log("Received a response of the signed copy: \(item.md5).", level: .debug)

        // Generate file name
        let path = makeOutputPath(app: app)
        logger.log("Output path: \(path).", level: .debug)

        // Download app package
        download(item: item, to: URL(fileURLWithPath: path))
        logger.log("Saved app package to \(URL(fileURLWithPath: path).lastPathComponent).", level: .info)

        // Apply patches
        applyPatches(item: item, email: account.email, path: path)
        logger.log("Done.", level: .info)
    }
}
