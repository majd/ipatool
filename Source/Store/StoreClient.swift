//
//  StoreClient.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation
import Networking

protocol StoreClientInterface {
    func authenticate(email: String, password: String, code: String?) async throws -> StoreResponse.Account
    func item(identifier: String, directoryServicesIdentifier: String) async throws -> StoreResponse.Item
}

final class StoreClient: StoreClientInterface {
    private let httpClient: HTTPClient
    
    init(httpClient: HTTPClient) {
        self.httpClient = httpClient
    }
    
    func authenticate(email: String, password: String, code: String?) async throws -> StoreResponse.Account {
        try await authenticate(email: email, password: password, code: code, isFirstAttempt: true)
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
    
    func item(identifier: String, directoryServicesIdentifier: String) async throws -> StoreResponse.Item {
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
}

extension StoreClient {
    enum Error: Swift.Error {
        case timeout
        case invalidResponse
    }
}
