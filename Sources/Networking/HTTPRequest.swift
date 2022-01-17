//
//  HTTPRequest.swift
//  Networking
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

public protocol HTTPRequest {
    var method: HTTPMethod { get }
    var endpoint: HTTPEndpoint { get }
    var headers: [String: String] { get }
    var payload: HTTPPayload? { get }
}

extension HTTPRequest {
    public var headers: [String: String] { [:] }
    public var payload: HTTPPayload? { nil }
}
