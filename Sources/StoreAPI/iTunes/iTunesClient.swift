//
//  iTunesClient.swift
//  StoreAPI
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation
import Networking

public protocol iTunesClientInterface {
    func lookup(
        bundleIdentifier: String,
        countryCode: String,
        deviceFamily: DeviceFamily
    ) throws -> iTunesResponse.Result

    func search(
        term: String,
        limit: Int,
        countryCode: String,
        deviceFamily: DeviceFamily
    ) throws -> [iTunesResponse.Result]
}

public final class iTunesClient: iTunesClientInterface {
    private let httpClient: HTTPClient
    
    public init(httpClient: HTTPClient) {
        self.httpClient = httpClient
    }
    
    public func lookup(
        bundleIdentifier: String,
        countryCode: String,
        deviceFamily: DeviceFamily
    ) throws -> iTunesResponse.Result {
        let request = iTunesRequest.lookup(
            bundleIdentifier: bundleIdentifier,
            countryCode: countryCode,
            deviceFamily: deviceFamily
        )
        let response = try httpClient.send(request)
        let decoded = try response.decode(iTunesResponse.self, as: .json)
        guard let result = decoded.results.first else { throw Error.appNotFound }
        return result
    }
    
    public func search(
        term: String,
        limit: Int,
        countryCode: String,
        deviceFamily: DeviceFamily
    ) throws -> [iTunesResponse.Result] {
        let request = iTunesRequest.search(
            term: term,
            limit: limit,
            countryCode: countryCode,
            deviceFamily: deviceFamily
        )
        let response = try httpClient.send(request)
        let decoded = try response.decode(iTunesResponse.self, as: .json)
        return decoded.results
    }
}

extension iTunesClient {
    public enum Error: Swift.Error {
        case timeout
        case appNotFound
    }
}
