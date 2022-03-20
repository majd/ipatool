//
//  StoreEndpoint.swift
//  StoreAPI
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation
import Networking

enum StoreEndpoint {
    case authenticate(prefix: String, guid: String)
    case download(guid: String)
    case buy
}

extension StoreEndpoint: HTTPEndpoint {
    var url: URL {
        var components = URLComponents(string: path)!
        components.scheme = "https"
        components.host = host
        return components.url!
    }
    
    private var host: String {
        switch self {
        case let .authenticate(prefix, _):
            return "\(prefix)-buy.itunes.apple.com"
        case .download:
            return "p25-buy.itunes.apple.com"
        case .buy:
            return "buy.itunes.apple.com"
        }
    }
    
    private var path: String {
        switch self {
        case let .authenticate(_, guid):
            return "/WebObjects/MZFinance.woa/wa/authenticate?guid=\(guid)"
        case let .download(guid):
            return "/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=\(guid)"
        case .buy:
            return "/WebObjects/MZBuy.woa/wa/buyProduct"
        }
    }
}
