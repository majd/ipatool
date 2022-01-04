//
//  iTunesClient.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

protocol iTunesClientInterface {
    func lookup(
        bundleIdentifier: String,
        country: String,
        deviceFamily: iTunesRequest.DeviceFamily
    ) async throws -> iTunesResponse.Result
    func search(
        term: String,
        limit: Int,
        country: String,
        deviceFamily: iTunesRequest.DeviceFamily
    ) async throws -> [iTunesResponse.Result]
}

final class iTunesClient: iTunesClientInterface {
    private let httpClient: HTTPClient
    
    init(httpClient: HTTPClient) {
        self.httpClient = httpClient
    }
    
    func lookup(
        bundleIdentifier: String,
        country: String,
        deviceFamily: iTunesRequest.DeviceFamily
    ) async throws -> iTunesResponse.Result {
        let request = iTunesRequest.lookup(
            bundleIdentifier: bundleIdentifier,
            country: country,
            deviceFamily: deviceFamily
        )
        let response = try await httpClient.send(request)
        let decoded = try response.decode(iTunesResponse.self, as: .json)
        guard let result = decoded.results.first else { throw Error.appNotFound }
        return result
    }
    
    func search(
        term: String,
        limit: Int,
        country: String,
        deviceFamily: iTunesRequest.DeviceFamily
    ) async throws -> [iTunesResponse.Result] {
        let request = iTunesRequest.search(term: term, limit: limit, country: country, deviceFamily: deviceFamily)
        let response = try await httpClient.send(request)
        let decoded = try response.decode(iTunesResponse.self, as: .json)
        return decoded.results
    }
}

extension iTunesClient {
    enum Error: Swift.Error {
        case timeout
        case appNotFound
    }
}
