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
    func item(identifier: String, directoryServicesIdentifier: String) async throws -> StoreResponse.Item
}

public final class StoreClient: StoreClientInterface {
    private let httpClient: HTTPClient
    
    public init(httpClient: HTTPClient) {
        self.httpClient = httpClient
    }
    
    public func authenticate(email: String, password: String, code: String?) async throws -> StoreResponse.Account {
        try await authenticate(email: email, password: password, code: code, isFirstAttempt: true)
    }
    
    public func item(identifier: String, directoryServicesIdentifier: String) async throws -> StoreResponse.Item {
        let request = StoreRequest.download(
            appIdentifier: identifier,
            directoryServicesIdentifier: directoryServicesIdentifier
        )
        let response = try await httpClient.send(request)
        let decoded = try response.decode(StoreResponse.self, as: .xml)

        switch decoded {
        case let .item(item):
            return item
        case .account:
            throw Error.invalidResponse
        case let .failure(error):
            throw error
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
        case .item:
            throw Error.invalidResponse
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
        }
    }
}

extension StoreClient {
    public enum Error: Swift.Error {
        case timeout
        case invalidResponse
    }
}
