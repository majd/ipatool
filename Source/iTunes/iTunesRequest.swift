//
//  iTunesRequest.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

enum iTunesRequest {
    case search(term: String, limit: Int)
    case lookup(bundleIdentifier: String)
}

extension iTunesRequest: HTTPRequest {
    var method: HTTPMethod {
        return .get
    }

    var endpoint: HTTPEndpoint {
        switch self {
        case .lookup:
            return iTunesEndpoint.lookup
        case .search:
            return iTunesEndpoint.search
        }
    }

    var payload: HTTPPayload? {
        switch self {
        case let .lookup(bundleIdentifier):
            return .urlEncoding(["media": "software", "bundleId": bundleIdentifier, "limit": "1"])
        case let .search(term, limit):
            return .urlEncoding(["media": "software", "term": term, "limit": "\(limit)"])
        }
    }
}
