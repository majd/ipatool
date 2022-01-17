//
//  iTunesResponse.swift
//  StoreAPI
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

public struct iTunesResponse {
    let results: [Result]
    let count: Int
}

extension iTunesResponse {
    public struct Result {
        public let bundleIdentifier: String
        public let version: String
        public let identifier: Int
        public let name: String
    }
}

extension iTunesResponse: Codable {
    enum CodingKeys: String, CodingKey {
        case count = "resultCount"
        case results
    }
}

extension iTunesResponse.Result: Codable {
    enum CodingKeys: String, CodingKey {
        case identifier = "trackId"
        case name = "trackName"
        case bundleIdentifier = "bundleId"
        case version
    }
}
