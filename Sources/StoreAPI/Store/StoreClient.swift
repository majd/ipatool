//
//  StoreClient.swift
//  StoreAPI
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation
import Networking

public protocol StoreClientInterface {
    func authenticate(email: String, password: String, code: String?) async throws -> StoreResponse.Account
    func item(identifier: String, directoryServicesIdentifier: String, passwordToken: String, country: String) async throws -> StoreResponse.Item
}

public final class StoreClient: StoreClientInterface {
    private let httpClient: HTTPClient
    
    public init(httpClient: HTTPClient) {
        self.httpClient = httpClient
    }
    
    public func authenticate(email: String, password: String, code: String?) async throws -> StoreResponse.Account {
        try await authenticate(email: email, password: password, code: code, isFirstAttempt: true)
    }
    
    public func item(identifier: String, directoryServicesIdentifier: String, passwordToken: String, country: String) async throws -> StoreResponse.Item {
        // Try to buy the app first…
        let buyRequest = StoreRequest.buy(
            appIdentifier: identifier,
            directoryServicesIdentifier: directoryServicesIdentifier,
            passwordToken: passwordToken,
            country: country
        )
        let buyResponse = try await httpClient.send(buyRequest)
        
        // If the Apple ID already owns the app, the endpoint will return a 500 error. We can ignore that here, since we'll then be able to just download the app in the next step without buying it again.
        if buyResponse.statusCode != 500 {
            let buyDecoded = try buyResponse.decode(StoreResponse.self, as: .xml)
            
            switch buyDecoded {
            case let .buyReceipt(receipt):
                if receipt.statusCode != 0 || receipt.statusType != "purchaseSuccess" {
                    throw Error.purchaseFailed
                }
            case let .failure(error):
                throw error
            default:
                throw Error.invalidResponse
            }
        }

        // …then download it.
        let downloadRequest = StoreRequest.download(
            appIdentifier: identifier,
            directoryServicesIdentifier: directoryServicesIdentifier
        )
        let downloadResponse = try await httpClient.send(downloadRequest)
        let downloadDecoded = try downloadResponse.decode(StoreResponse.self, as: .xml)

        switch downloadDecoded {
        case let .item(item):
            return item
        case let .failure(error):
            throw error
        default:
            throw Error.invalidResponse
        }
    }
    
    private func authenticate(email: String,
                              password: String,
                              code: String?,
                              isFirstAttempt: Bool) async throws -> StoreResponse.Account {
        let request = StoreRequest.authenticate(email: email, password: password, code: code)
        let response = try await httpClient.send(request)
        let decoded = try response.decode(StoreResponse.self, as: .xml)

        switch decoded {
        case let .account(account):
            return account
        case let .failure(error):
            switch error {
            case StoreResponse.Error.invalidCredentials:
                if isFirstAttempt {
                    return try await authenticate(email: email, password: password, code: code, isFirstAttempt: false)
                }

                throw error
            default:
                throw error
            }
        default:
            throw Error.invalidResponse
        }
    }
}

extension StoreClient {
    public enum Error: Swift.Error {
        case timeout
        case invalidResponse
        case purchaseFailed
    }
}
