//
//  iTunesRequest.swift
//  StoreAPI
//
//  Created by Majd Alfhaily on 22.05.21.
//

import ArgumentParser
import Networking

enum iTunesRequest {
    case search(term: String, limit: Int, country: String, deviceFamily: DeviceFamily = .phone)
    case lookup(bundleIdentifier: String, country: String, deviceFamily: DeviceFamily = .phone)
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
        case let .lookup(bundleIdentifier, country, deviceFamily):
            return .urlEncoding([
                "media": "software",
                "bundleId": bundleIdentifier,
                "limit": "1",
                "country": country,
                "entity": deviceFamily.entity
            ])
        case let .search(term, limit, country, deviceFamily):
            return .urlEncoding([
                "media": "software",
                "term": term,
                "limit": "\(limit)",
                "country": country,
                "entity": deviceFamily.entity
            ])
        }
    }
}

private extension DeviceFamily {
    var entity: String {
        switch self {
        case .phone:
            return "software"
        case .pad:
            return "iPadSoftware"
        }
    }
}
