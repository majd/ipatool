//
//  StoreClient.swift
//  StoreAPI
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation
import Networking

public protocol StoreClientInterface {
    func authenticate(email: String, password: String, code: String?) throws -> StoreResponse.Account
    func item(identifier: String, directoryServicesIdentifier: String) throws -> StoreResponse.Item
    func purchase(
        identifier: String,
        directoryServicesIdentifier: String,
        passwordToken: String,
        countryCode: String
    ) throws
}

public final class StoreClient: StoreClientInterface {
    private let httpClient: HTTPClient
    
    public init(httpClient: HTTPClient) {
        self.httpClient = httpClient
    }
    
    public func authenticate(email: String, password: String, code: String?) throws -> StoreResponse.Account {
        try authenticate(email: email, password: password, code: code, isFirstAttempt: true)
    }
    
    public func item(identifier: String, directoryServicesIdentifier: String) throws -> StoreResponse.Item {
        let request = StoreRequest.download(
            appIdentifier: identifier,
            directoryServicesIdentifier: directoryServicesIdentifier
        )
        let response = try httpClient.send(request)
        let storeResponse = try response.decode(StoreResponse.self, as: .xml)

        switch storeResponse {
        case let .item(item):
            return item
        case let .failure(error):
            throw error
        default:
            throw Error.invalidResponse
        }
    }

    public func purchase(
        identifier: String,
        directoryServicesIdentifier: String,
        passwordToken: String,
        countryCode: String
    ) throws {
        let request = StoreRequest.purchase(
            appIdentifier: identifier,
            directoryServicesIdentifier: directoryServicesIdentifier,
            passwordToken: passwordToken,
            countryCode: countryCode
        )

        let response = try httpClient.send(request)

        // Returns status code 500 if the Apple ID already contains a license
        switch response.statusCode {
        case 500:
            throw Error.duplicateLicense
        default:
            let response = try response.decode(StoreResponse.self, as: .xml)

            switch response {
            case let .purchase(receipt):
                guard receipt.statusCode == 0, receipt.status == .success else {
                    throw Error.purchaseFailed
                }
            case let .failure(error):
                throw error
            default:
                throw Error.invalidResponse
            }
        }
    }
    
    private func authenticate(email: String,
                              password: String,
                              code: String?,
                              isFirstAttempt: Bool) throws -> StoreResponse.Account {
        let request = StoreRequest.authenticate(email: email, password: password, code: code)
        let response = try httpClient.send(request)
        let decoded = try response.decode(StoreResponse.self, as: .xml)

        switch decoded {
        case let .account(account):
            return account
        case let .failure(error):
            switch error {
            case StoreResponse.Error.invalidCredentials:
                if isFirstAttempt {
                    return try authenticate(email: email, password: password, code: code, isFirstAttempt: false)
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
        case duplicateLicense
    }
}
