//
//  HTTPResponse.swift
//  ipatool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

struct HTTPResponse {
    let statusCode: Int
    let data: Data?
}

extension HTTPResponse {
    func decode<T: Decodable>(_ type: T.Type, as decoder: Decoder) throws -> T {
        guard let data = data else {
            throw Error.noData
        }
        
        switch decoder {
        case .json:
            return try JSONDecoder().decode(type, from: data)
        case .propertyList:
            return try PropertyListDecoder().decode(type, from: data)
        }
    }
}

extension HTTPResponse {
    enum Decoder {
        case json
        case propertyList
    }
    
    enum Error: Swift.Error {
        case noData
    }
}
