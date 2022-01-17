//
//  HTTPClient.swift
//  Networking
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

public protocol HTTPClientInterface {
    func send(_ request: HTTPRequest) async throws -> HTTPResponse
}

public final class HTTPClient: HTTPClientInterface {
    private let session: URLSessionInterface
    
    public init(session: URLSessionInterface) {
        self.session = session
    }

    public func send(_ request: HTTPRequest) async throws -> HTTPResponse {
        let request = try makeURLRequest(from: request)
        let (data, response) = try await session.data(for: request)

        guard let response = response as? HTTPURLResponse else {
            throw Error.invalidResponse(response)
        }

        return HTTPResponse(statusCode: response.statusCode, data: data)
    }
    
    private func makeURLRequest(from request: HTTPRequest) throws -> URLRequest {
        var urlRequest = URLRequest(url: request.endpoint.url)
        urlRequest.httpMethod = request.method.rawValue

        switch request.payload {
        case .none:
            urlRequest.httpBody = nil
        case let .urlEncoding(propertyList):
            urlRequest.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")

            var urlComponents = URLComponents(string: request.endpoint.url.absoluteString)
            urlComponents?.queryItems = !propertyList.isEmpty ? propertyList.map {
                URLQueryItem(name: $0.0, value: $0.1.description)
            } : nil

            switch request.method {
            case .get:
                urlRequest.url = urlComponents?.url
            case .post:
                urlRequest.httpBody = urlComponents?.percentEncodedQuery?.data(
                    using: .utf8,
                    allowLossyConversion: false
                )
            }
        case let .xml(value):
            urlRequest.setValue("application/xml", forHTTPHeaderField: "Content-Type")
            urlRequest.httpBody = try PropertyListSerialization.data(fromPropertyList: value, format: .xml, options: 0)
        }
        
        request.headers.forEach { urlRequest.setValue($0.value, forHTTPHeaderField: $0.key) }

        return urlRequest
    }
}

extension HTTPClient {
    enum Error: Swift.Error {
        case invalidResponse(URLResponse?)
        case timeout
    }
}
