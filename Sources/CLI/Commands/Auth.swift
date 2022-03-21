//
//  Auth.swift
//  IPATool
//
//  Created by Majd Alfhaily on 21.03.22.
//

import ArgumentParser
import Foundation
import Networking
import StoreAPI
import Persistence

struct Auth: AsyncParsableCommand {
    static var configuration: CommandConfiguration {
        return .init(
            commandName: "auth",
            abstract: "Authenticate with the App Store.",
            subcommands: [Login.self, Revoke.self],
            defaultSubcommand: nil
        )
    }
}

extension Auth {
    struct Login: AsyncParsableCommand {
        static var configuration: CommandConfiguration {
            return .init(abstract: "Login to the App Store.")
        }

        @Option(name: [.short, .customLong("email")], help: "The email address for the Apple ID.")
        private var emailArgument: String?

        @Option(name: [.short, .customLong("password")], help: "The password for the Apple ID.")
        private var passwordArgument: String?

        @Option(name: [.customLong("auth-code")], help: "The 2FA code for the Apple ID.")
        private var authCodeArgument: String?

        @Option(name: [.long], help: "The log level.")
        private var logLevel: LogLevel = .info

        lazy var logger = ConsoleLogger(level: logLevel)
    }

    struct Revoke: AsyncParsableCommand {
        static var configuration: CommandConfiguration {
            return .init(abstract: "Revoke your App Store credentials.")
        }

        @Option(name: [.long], help: "The log level.")
        private var logLevel: LogLevel = .info

        lazy var logger = ConsoleLogger(level: logLevel)
    }
}

extension Auth.Login {
    private mutating func email() -> String {
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

    private mutating func password() -> String {
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

    private mutating func authCode() -> String {
        if let authCode = authCodeArgument {
            return authCode
        } else if let authCode = ProcessInfo.processInfo.environment["IPATOOL_2FA_CODE"] {
            return authCode
        } else if let authCode = String(validatingUTF8: UnsafePointer<CChar>(getpass(logger.compile("Enter 2FA code: ", level: .warning)))) {
            return authCode
        } else {
            logger.log("A 2FA auth-code is required.", level: .error)
            _exit(1)
        }
    }

    private mutating func authenticate(email: String, password: String) async -> Account {
        logger.log("Creating HTTP client...", level: .debug)
        let httpClient = HTTPClient(session: URLSession.shared)

        logger.log("Creating App Store client...", level: .debug)
        let storeClient = StoreClient(httpClient: httpClient)

        do {
            logger.log("Authenticating with the App Store...", level: .info)
            let account = try await storeClient.authenticate(email: email, password: password, code: nil)
            return Account(
                name: "\(account.firstName) \(account.lastName)",
                email: email,
                passwordToken: account.passwordToken,
                directoryServicesIdentifier: account.directoryServicesIdentifier
            )
        } catch {
            switch error {
            case StoreResponse.Error.codeRequired:
                do {
                    let account = try await storeClient.authenticate(email: email, password: password, code: authCode())
                    return Account(
                        name: "\(account.firstName) \(account.lastName)",
                        email: email,
                        passwordToken: account.passwordToken,
                        directoryServicesIdentifier: account.directoryServicesIdentifier
                    )
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

    mutating func run() async throws {
        // Get Apple ID email
        let email: String = email()

        // Get Apple ID password
        let password: String = password()

        // Authenticate with the App Store
        let account: Account = await authenticate(email: email, password: password)

        // Store data in keychain
        do {
            let keychainStore = KeychainStore(service: "ipatool.service")
            try keychainStore.setValue(account, forKey: "account")

            logger.log("Authenticated as '\(account.name)'.", level: .info)
        } catch {
            logger.log("Failed to save account data in keychain.", level: .error)
            logger.log("\(error)", level: .debug)

            _exit(1)
        }
    }
}

extension Auth.Revoke {
    mutating func run() async throws {
        let keychainStore = KeychainStore(service: "ipatool.service")

        guard let account: Account = try keychainStore.value(forKey: "account") else {
            logger.log("No credentials available to revoke.", level: .error)
            _exit(1)
        }

        try keychainStore.remove("account")
        logger.log("Revoked credentials for '\(account.name)'.", level: .info)
    }
}
