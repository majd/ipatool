//
//  iTunesEndpoint.swift
//  StoreAPI
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation
import Networking

enum iTunesEndpoint {
    case search
    case lookup
}

extension iTunesEndpoint: HTTPEndpoint {
    var url: URL {
        var components = URLComponents(string: path)!
        components.scheme = "https"
        components.host = "itunes.apple.com"
        return components.url!
    }

    private var path: String {
        switch self {
        case .search:
            return "/search"
        case .lookup:
            return "/lookup"
        }
    }
}
