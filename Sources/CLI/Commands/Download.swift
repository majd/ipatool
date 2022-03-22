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

struct Download: AsyncParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(abstract: "Download (encrypted) iOS app packages from the App Store.")
    }

    @Option(name: [.short, .long], help: "The bundle identifier of the target iOS app.")
    private var bundleIdentifier: String

    @Option(name: [.short, .long], help: "The two-letter (ISO 3166-1 alpha-2) country code for the iTunes Store.")
    private var country: String = "US"

    @Option(name: [.short, .long], help: "The device family to limit the search query to.")
    private var deviceFamily: DeviceFamily = .phone

    @Option(name: [.short, .long], help: "The destination path of the downloaded app package.")
    private var output: String?

    @Option(name: [.long], help: "The log level.")
    private var logLevel: LogLevel = .info

    lazy var logger = ConsoleLogger(level: logLevel)
}

extension Download {
    private mutating func app(with bundleIdentifier: String, country: String) async -> iTunesResponse.Result {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(session: URLSession.shared)

        logger.log("Creating iTunes client...", level: .debug)
        let itunesClient = iTunesClient(httpClient: httpClient)

        do {
            logger.log("Querying the iTunes Store for '\(bundleIdentifier)' in country '\(country)'...", level: .info)
            return try await itunesClient.lookup(
                bundleIdentifier: bundleIdentifier,
                country: country,
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

    private mutating func item(from app: iTunesResponse.Result, account: Account) async -> StoreResponse.Item {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(session: URLSession.shared)

        logger.log("Creating App Store client...", level: .debug)
        let storeClient = StoreClient(httpClient: httpClient)

        do {
            logger.log("Requesting a signed copy of '\(app.identifier)' from the App Store...", level: .info)
            return try await storeClient.item(
                identifier: "\(app.identifier)",
                directoryServicesIdentifier: account.directoryServicesIdentifier,
                passwordToken: account.passwordToken,
                country: country
            )
        } catch {
            logger.log("\(error)", level: .debug)
            
            switch error {
            case StoreClient.Error.invalidResponse:
                logger.log("Received invalid response.", level: .error)
            case StoreClient.Error.purchaseFailed:
                logger.log("Buying the app failed.", level: .error)
            case StoreResponse.Error.invalidItem:
                logger.log("Received invalid store item.", level: .error)
            case StoreResponse.Error.invalidLicense:
                logger.log("Your Apple ID does not have a license for this app. Download the app on an iOS device to obtain a license.", level: .error)
            case StoreResponse.Error.wrongCountry:
                logger.log("Your Apple ID is not valid for the country you specified.", level: .error)
            default:
                logger.log("An unknown error has occurred.", level: .error)
            }
            
            _exit(1)
        }
    }
    
    private mutating func download(item: StoreResponse.Item, to targetURL: URL) async {
        logger.log("Creating download client...", level: .debug)
        let downloadClient = HTTPDownloadClient()

        do {
            logger.log("Downloading app package...", level: .info)
            try await downloadClient.download(from: item.url, to: targetURL) { [logger] progress in
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
    
    mutating func run() async throws {
        // Authenticate with the App Store
        let keychainStore = KeychainStore(service: "ipatool.service")

        guard let account: Account = try keychainStore.value(forKey: "account") else {
            logger.log("Authentication required. Run \"ipatool auth --help\" for help.", level: .error)
            _exit(1)
        }
        logger.log("Authenticated as '\(account.name)'.", level: .info)

        // Query for app
        let app: iTunesResponse.Result = await app(with: bundleIdentifier, country: country)
        logger.log("Found app: \(app.name) (\(app.version)).", level: .debug)

        // Query for store item
        let item: StoreResponse.Item = await item(from: app, account: account)
        logger.log("Received a response of the signed copy: \(item.md5).", level: .debug)

        // Generate file name
        let path = makeOutputPath(app: app)
        logger.log("Output path: \(path).", level: .debug)

        // Download app package
        await download(item: item, to: URL(fileURLWithPath: path))
        logger.log("Saved app package to \(URL(fileURLWithPath: path).lastPathComponent).", level: .info)

        // Apply patches
        applyPatches(item: item, email: account.email, path: path)
        logger.log("Done.", level: .info)
    }
}
