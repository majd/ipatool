//
//  Download.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import ArgumentParser
import Foundation

struct Download: ParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(abstract: "Download (encrypted) iOS app packages from the App Store.")
    }

    @Option(name: [.short, .long], help: "The bundle identifier of the target iOS app.")
    private var bundleIdentifier: String

    @Option(name: [.short, .long], help: "The country of the target iOS app.")
    private var country: String = "US"

    @Option(name: [.short, .customLong("email")], help: "The email address for the Apple ID.")
    private var emailArgument: String?

    @Option(name: [.short, .customLong("password")], help: "The password for the Apple ID.")
    private var passwordArgument: String?

    @Option
    private var logLevel: LogLevel = .info
    
    lazy var logger = ConsoleLogger(level: logLevel)
}

extension Download {
    mutating func app(with bundleIdentifier: String, country: String) -> iTunesResponse.Result {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(urlSession: URLSession.shared)

        logger.log("Creating iTunes client...", level: .debug)
        let itunesClient = iTunesClient(httpClient: httpClient)

        do {
            logger.log("Querying the iTunes Store for '\(bundleIdentifier)' in country '\(country)'...", level: .info)
            return try itunesClient.lookup(bundleIdentifier: bundleIdentifier, country: country)
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
    
    mutating func email() -> String {
        if let email = emailArgument {
            return email
        } else if let email = ProcessInfo.processInfo.environment["IPATOOL_EMAIL"] {
            return email
        } else if let email = String(validatingUTF8: UnsafePointer<CChar>(getpass(logger.compile("Enter Apple ID email: ", level: .warning)))) {
            return email
        } else {
            logger.log("An Apple ID email address is required.", level: .error)
            _exit(1)
        }
    }
    
    mutating func password() -> String {
        if let password = passwordArgument {
            return password
        } else if let password = ProcessInfo.processInfo.environment["IPATOOL_PASSWORD"] {
            return password
        } else if let password = String(validatingUTF8: UnsafePointer<CChar>(getpass(logger.compile("Enter Apple ID password: ", level: .warning)))) {
            return password
        } else {
            logger.log("An Apple ID password is required.", level: .error)
            _exit(1)
        }
    }
    
    mutating func authenticate(email: String, password: String) -> StoreResponse.Account {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(urlSession: URLSession.shared)

        logger.log("Creating App Store client...", level: .debug)
        let storeClient = StoreClient(httpClient: httpClient)

        do {
            logger.log("Authenticating with the App Store...", level: .info)
            return try storeClient.authenticate(email: email, password: password)
        } catch {
            switch error {
            case StoreResponse.Error.codeRequired:
                let code = String(validatingUTF8: UnsafePointer<CChar>(getpass(logger.compile("Enter 2FA code: ", level: .warning))))
                
                do {
                    return try storeClient.authenticate(email: email, password: password, code: code)
                } catch {
                    logger.log("\(error)", level: .debug)
                    
                    switch error {
                    case StoreClient.Error.invalidResponse:
                        logger.log("Received invalid response.", level: .error)
                    case StoreResponse.Error.invalidAccount:
                        logger.log("This Apple ID has not been set up to use the App Store.", level: .error)
                    case StoreResponse.Error.invalidCredentials:
                        logger.log("Invalid credentials.", level: .error)
                    case StoreResponse.Error.lockedAccount:
                        logger.log("This Apple ID has been disabled for security reasons.", level: .error)
                    default:
                        logger.log("An unknown error has occurred.", level: .error)
                    }
                    
                    _exit(1)
                }
            default:
                logger.log("\(error)", level: .debug)
                
                switch error {
                case StoreClient.Error.invalidResponse:
                    logger.log("Received invalid response.", level: .error)
                case StoreResponse.Error.invalidAccount:
                    logger.log("This Apple ID has not been set up to use the App Store.", level: .error)
                case StoreResponse.Error.invalidCredentials:
                    logger.log("Invalid credentials.", level: .error)
                case StoreResponse.Error.lockedAccount:
                    logger.log("This Apple ID has been disabled for security reasons.", level: .error)
                default:
                    logger.log("An unknown error has occurred.", level: .error)
                }
                
                _exit(1)
            }
        }

    }
    
    mutating func item(from app: iTunesResponse.Result, account: StoreResponse.Account) -> StoreResponse.Item {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(urlSession: URLSession.shared)

        logger.log("Creating App Store client...", level: .debug)
        let storeClient = StoreClient(httpClient: httpClient)

        do {
            logger.log("Requesting a signed copy of '\(app.identifier)' from the App Store...", level: .info)
            return try storeClient.item(identifier: "\(app.identifier)", directoryServicesIdentifier: account.directoryServicesIdentifier)
        } catch {
            logger.log("\(error)", level: .debug)
            
            switch error {
            case StoreClient.Error.invalidResponse:
                logger.log("Received invalid response.", level: .error)
            case StoreResponse.Error.invalidItem:
                logger.log("Received invalid store item.", level: .error)
            case StoreResponse.Error.invalidLicense:
                logger.log("Your Apple ID does not have a license for this app. Download the app on an iOS device to obtain a license.", level: .error)
            default:
                logger.log("An unknown error has occurred.", level: .error)
            }
            
            _exit(1)
        }
    }
    
    mutating func download(item: StoreResponse.Item, to targetURL: URL) {
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
    
    mutating func applyPatches(item: StoreResponse.Item, email: String, path: String) {
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
    
    mutating func run() throws {
        // Query for app
        let app: iTunesResponse.Result = app(with: bundleIdentifier, country: country)
        logger.log("Found app: \(app.name) (\(app.version)).", level: .debug)
        
        // Get Apple ID email
        let email: String = email()

        // Get Apple ID password
        let password: String = password()

        // Authenticate with the App Store
        let account: StoreResponse.Account = authenticate(email: email, password: password)
        logger.log("Authenticated as '\(account.firstName) \(account.lastName)'.", level: .info)

        // Query for store item
        let item: StoreResponse.Item = item(from: app, account: account)
        logger.log("Received a response of the signed copy: \(item.md5).", level: .debug)

        // Generate file name
        let path = FileManager.default.currentDirectoryPath
            .appending("/\(bundleIdentifier)_\(app.identifier)_v\(app.version)_\(Int.random(in: 100...999))")
            .appending(".ipa")
        logger.log("Output path: \(path).", level: .debug)

        // Download app package
        download(item: item, to: URL(fileURLWithPath: path))
        logger.log("Saved app package to \(URL(fileURLWithPath: path).lastPathComponent).", level: .info)

        // Apply patches
        applyPatches(item: item, email: email, path: path)
        logger.log("Done.", level: .info)
    }
}
