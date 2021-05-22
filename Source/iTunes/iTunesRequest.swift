//
//  iTunesRequest.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

enum iTunesRequest {
    case lookup(bundleIdentifier: String)
}

extension iTunesRequest: HTTPRequest {
    var endpoint: HTTPEndpoint {
        return iTunesEndpoint.lookup        
    }

    var method: HTTPMethod {
        return .get
    }

    var payload: HTTPPayload? {
        switch self {
        case let .lookup(bundleIdentifier):
            return .urlEncoding(["media": "software", "bundleId": bundleIdentifier, "limit": "1"])
        }
    }
}
