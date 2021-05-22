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

    @Option(name: [.short, .long], help: "The email address for the Apple ID.")
    private var email: String?

    @Option(name: [.short, .long], help: "The password for the Apple ID.")
    private var password: String?

    @Option
    private var logLevel: LogLevel = .info
}

extension Download {
    func run() throws {
        let logger = ConsoleLogger(level: logLevel)
        
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(urlSession: URLSession.shared)

        logger.log("Creating iTunes client...", level: .debug)
        let itunesClient = iTunesClient(httpClient: httpClient)

        logger.log("Creating App Store client...", level: .debug)
        let storeClient = StoreClient(httpClient: httpClient)

        logger.log("Creating download client...", level: .debug)
        let downloadClient = HTTPDownloadClient()

        logger.log("Querying the iTunes store for '\(bundleIdentifier)'...", level: .info)
        let app: iTunesResponse.Result
        
        do {
            app = try itunesClient.lookup(bundleIdentifier: bundleIdentifier)
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
        logger.log("Found app: \(app.name) (\(app.version)).", level: .debug)
        
        let email: String
        let password: String
        
        if let cliEmail = self.email {
            email = cliEmail
        } else if let envEmail = ProcessInfo.processInfo.environment["IPATOOL_EMAIL"] {
            email = envEmail
        } else if let inputEmail = String(validatingUTF8: UnsafePointer<CChar>(getpass(logger.compile("Enter Apple ID email: ", level: .warning)))) {
            email = inputEmail
        } else {
            logger.log("An Apple ID email address is required.", level: .error)
            _exit(1)
        }
        
        if let cliPassword = self.password {
            password = cliPassword
        } else if let envPassword = ProcessInfo.processInfo.environment["IPATOOL_PASSWORD"] {
            password = envPassword
        } else if let inputPassword = String(validatingUTF8: UnsafePointer<CChar>(getpass(logger.compile("Enter Apple ID password: ", level: .warning)))) {
            password = inputPassword
        } else {
            logger.log("An Apple ID password is required.", level: .error)
            _exit(1)
        }

        let account: StoreResponse.Account

        do {
            logger.log("Authenticating with the App Store...", level: .info)
            account = try storeClient.authenticate(email: email, password: password)
        } catch {
            switch error {
            case StoreResponse.Error.codeRequired:
                let code = String(validatingUTF8: UnsafePointer<CChar>(getpass(logger.compile("Enter 2FA code: ", level: .warning))))
                
                do {
                    account = try storeClient.authenticate(email: email, password: password, code: code)
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
        logger.log("Authenticated as '\(account.firstName) \(account.lastName)'.", level: .info)

        logger.log("Requesting a signed copy of '\(app.identifier)' from the App Store...", level: .info)
        let item = try storeClient.item(identifier: "\(app.identifier)", directoryServicesIdentifier: account.directoryServicesIdentifier)
        logger.log("Received a response of the signed copy: \(item.md5).", level: .debug)
        
        logger.log("Creating signature client...", level: .debug)
        let path = FileManager.default.currentDirectoryPath
            .appending("/\(bundleIdentifier)_\(app.identifier)_v\(app.version)_\(Int.random(in: 100...999))")
            .appending(".ipa")

        logger.log("Output path: \(path).", level: .debug)
        let signatureClient = SignatureClient(fileManager: .default, filePath: path)

        logger.log("Downloading app package...", level: .info)
        try downloadClient.download(from: item.url, to: URL(fileURLWithPath: path)) { progress in
            logger.log("Downloading app package... [\(Int((progress * 100).rounded()))%]",
                       prefix: "\u{1B}[1A\u{1B}[K",
                       level: .info)
        }
        logger.log("Saved app package to \(URL(fileURLWithPath: path).lastPathComponent).", level: .info)

        logger.log("Applying patches...", level: .info)
        try signatureClient.appendMetadata(item: item, email: email)
        try signatureClient.appendSignature(item: item)
        
        logger.log("Done.", level: .info)
    }
}
