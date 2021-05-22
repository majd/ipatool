//
//  StoreClient.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

protocol StoreClientInterface {
    func authenticate(email: String, password: String, code: String?, completion: @escaping (Result<StoreResponse.Account, Error>) -> Void)
    func item(identifier: String, directoryServicesIdentifier: String, completion: @escaping (Result<StoreResponse.Item, Error>) -> Void)
}

extension StoreClientInterface {
    func authenticate(email: String,
                      password: String,
                      code: String? = nil,
                      completion: @escaping (Result<StoreResponse.Account, Swift.Error>) -> Void) {
        authenticate(email: email, password: password, code: code, completion: completion)
    }
    
    func authenticate(email: String, password: String, code: String? = nil) throws -> StoreResponse.Account {
        let semaphore = DispatchSemaphore(value: 0)
        var result: Result<StoreResponse.Account, Error>?
        
        authenticate(email: email, password: password, code: code) {
            result = $0
            semaphore.signal()
        }
        
        _ = semaphore.wait(timeout: .distantFuture)
        
        switch result {
        case .none:
            throw StoreClient.Error.timeout
        case let .failure(error):
            throw error
        case let .success(result):
            return result
        }
    }
    
    func item(identifier: String, directoryServicesIdentifier: String) throws -> StoreResponse.Item {
        let semaphore = DispatchSemaphore(value: 0)
        var result: Result<StoreResponse.Item, Error>?
        
        item(identifier: identifier, directoryServicesIdentifier: directoryServicesIdentifier) {
            result = $0
            semaphore.signal()
        }
        
        _ = semaphore.wait(timeout: .distantFuture)
        
        switch result {
        case .none:
            throw StoreClient.Error.timeout
        case let .failure(error):
            throw error
        case let .success(result):
            return result
        }
    }
}

final class StoreClient: StoreClientInterface {
    private let httpClient: HTTPClient
    
    init(httpClient: HTTPClient) {
        self.httpClient = httpClient
    }
    
    func authenticate(email: String, password: String, code: String?, completion: @escaping (Result<StoreResponse.Account, Swift.Error>) -> Void) {
        authenticate(email: email,
                     password: password,
                     code: code,
                     isFirstAttempt: true,
                     completion: completion)
    }
    
    private func authenticate(email: String,
                              password: String,
                              code: String?,
                              isFirstAttempt: Bool,
                              completion: @escaping (Result<StoreResponse.Account, Swift.Error>) -> Void) {
        let request = StoreRequest.authenticate(email: email, password: password, code: code)
        
        httpClient.send(request) { [weak self] result in
            switch result {
            case let .success(response):
                do {
                    let decoded = try response.decode(StoreResponse.self, as: .xml)

                    switch decoded {
                    case let .account(account):
                        completion(.success(account))
                    case .item:
                        completion(.failure(Error.invalidResponse))
                    case let .failure(error):
                        switch error {
                        case StoreResponse.Error.invalidCredentials:
                            if isFirstAttempt {
                                return self?.authenticate(email: email,
                                                          password: password,
                                                          code: code,
                                                          isFirstAttempt: false,
                                                          completion: completion) ?? ()
                            }

                            completion(.failure(error))
                        default:
                            completion(.failure(error))
                        }
                    }
                } catch {
                    completion(.failure(error))
                }
            case let .failure(error):
                completion(.failure(error))
            }
        }
    }
    
    func item(identifier: String, directoryServicesIdentifier: String, completion: @escaping (Result<StoreResponse.Item, Swift.Error>) -> Void) {
        let request = StoreRequest.download(appIdentifier: identifier, directoryServicesIdentifier: directoryServicesIdentifier)
        
        httpClient.send(request) { result in
            switch result {
            case let .success(response):
                do {
                    let decoded = try response.decode(StoreResponse.self, as: .xml)

                    switch decoded {
                    case let .item(item):
                        completion(.success(item))
                    case .account:
                        completion(.failure(Error.invalidResponse))
                    case let .failure(error):
                        completion(.failure(error))
                    }
                } catch {
                    completion(.failure(error))
                }
            case let .failure(error):
                completion(.failure(error))
            }
        }
    }
}

extension StoreClient {
    enum Error: Swift.Error {
        case timeout
        case invalidResponse
    }
}
