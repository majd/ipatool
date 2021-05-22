//
//  HTTPRequest.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

protocol HTTPRequest {
    var method: HTTPMethod { get }
    var endpoint: HTTPEndpoint { get }
    var headers: [String: String] { get }
    var payload: HTTPPayload? { get }
}

extension HTTPRequest {
    var headers: [String: String] { [:] }
    var payload: HTTPPayload? { nil }
}
