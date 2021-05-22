//
//  iTunesResponse.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

struct iTunesResponse {
    let results: [Result]
    let count: Int
}

extension iTunesResponse {
    struct Result {
        let bundleIdentifier: String
        let version: String
        let identifier: Int
        let name: String
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
