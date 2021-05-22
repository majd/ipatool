//
//  iTunesClient.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

protocol iTunesClientInterface {
    func lookup(bundleIdentifier: String, completion: @escaping (Result<iTunesResponse.Result, Error>) -> Void)
    func search(term: String, limit: Int, completion: @escaping (Result<[iTunesResponse.Result], Error>) -> Void)
}

extension iTunesClientInterface {
    func lookup(bundleIdentifier: String) throws -> iTunesResponse.Result {
        let semaphore = DispatchSemaphore(value: 0)
        var result: Result<iTunesResponse.Result, Error>?
        
        lookup(bundleIdentifier: bundleIdentifier) {
            result = $0
            semaphore.signal()
        }
        
        _ = semaphore.wait(timeout: .distantFuture)
        
        switch result {
        case .none:
            throw iTunesClient.Error.timeout
        case let .failure(error):
            throw error
        case let .success(result):
            return result
        }
    }
    
    func search(term: String, limit: Int) throws -> [iTunesResponse.Result] {
        let semaphore = DispatchSemaphore(value: 0)
        var result: Result<[iTunesResponse.Result], Error>?
        
        search(term: term, limit: limit) {
            result = $0
            semaphore.signal()
        }
        
        _ = semaphore.wait(timeout: .distantFuture)
        
        switch result {
        case .none:
            throw iTunesClient.Error.timeout
        case let .failure(error):
            throw error
        case let .success(result):
            return result
        }
    }
}

final class iTunesClient: iTunesClientInterface {
    private let httpClient: HTTPClient
    
    init(httpClient: HTTPClient) {
        self.httpClient = httpClient
    }
    
    func lookup(bundleIdentifier: String, completion: @escaping (Result<iTunesResponse.Result, Swift.Error>) -> Void) {
        let request = iTunesRequest.lookup(bundleIdentifier: bundleIdentifier)
        
        httpClient.send(request) { result in
            switch result {
            case let .success(response):
                do {
                    let decoded = try response.decode(iTunesResponse.self, as: .json)
                    guard let result = decoded.results.first else { return completion(.failure(Error.appNotFound)) }
                    completion(.success(result))
                } catch {
                    completion(.failure(error))
                }
            case let .failure(error):
                completion(.failure(error))
            }
        }
    }
    
    func search(term: String, limit: Int, completion: @escaping (Result<[iTunesResponse.Result], Swift.Error>) -> Void) {
        let request = iTunesRequest.search(term: term, limit: limit)
        
        httpClient.send(request) { result in
            switch result {
            case let .success(response):
                do {
                    let decoded = try response.decode(iTunesResponse.self, as: .json)
                    completion(.success(decoded.results))
                } catch {
                    completion(.failure(error))
                }
            case let .failure(error):
                completion(.failure(error))
            }
        }
    }
}

extension iTunesClient {
    enum Error: Swift.Error {
        case timeout
        case appNotFound
    }
}
