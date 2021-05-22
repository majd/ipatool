//
//  iTunesEndpoint.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

enum iTunesEndpoint {
    case lookup
}

extension iTunesEndpoint: HTTPEndpoint {
    var url: URL {
        var components = URLComponents(string: "/lookup")!
        components.scheme = "https"
        components.host = "itunes.apple.com"
        return components.url!
    }
}
