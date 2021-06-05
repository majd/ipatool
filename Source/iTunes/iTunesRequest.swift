//
//  iTunesRequest.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

enum iTunesRequest {
    case search(term: String, limit: Int, country: String)
    case lookup(bundleIdentifier: String, country: String)
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
        case let .lookup(bundleIdentifier, country):
            return .urlEncoding(["media": "software", "bundleId": bundleIdentifier, "limit": "1", "country": country])
        case let .search(term, limit, country):
            return .urlEncoding(["media": "software", "term": term, "limit": "\(limit)", "country": country])
        }
    }
}
