//
//  HTTPResponse.swift
//  Networking
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

public struct HTTPResponse {
    public let statusCode: Int
    let data: Data?
}

extension HTTPResponse {
    public func decode<T: Decodable>(_ type: T.Type, as decoder: Decoder) throws -> T {
        guard let data = data else {
            throw Error.noData
        }
        
        switch decoder {
        case .json:
            let decoder = JSONDecoder()
            decoder.userInfo = [.init(rawValue: "data")!: data]
            return try decoder.decode(type, from: data)
        case .xml:
            let decoder = PropertyListDecoder()
            decoder.userInfo = [.init(rawValue: "data")!: data]
            return try decoder.decode(type, from: data)
        }
    }
}

extension HTTPResponse {
    public enum Decoder {
        case json
        case xml
    }
    
    public enum Error: Swift.Error {
        case noData
    }
}
