//
//  HTTPClient.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

protocol HTTPClientInterface {
    func send(_ request: HTTPRequest, completion: @escaping (Result<HTTPResponse, Error>) -> Void)
}

extension HTTPClientInterface {
    func send(_ request: HTTPRequest) throws -> HTTPResponse {
        let semaphore = DispatchSemaphore(value: 0)
        var result: Result<HTTPResponse, Error>?
        
        send(request) {
            result = $0
            semaphore.signal()
        }
        
        _ = semaphore.wait(timeout: .distantFuture)
        
        switch result {
        case .none:
            throw HTTPClient.Error.timeout
        case let .failure(error):
            throw error
        case let .success(response):
            return response
        }
    }
}

final class HTTPClient: HTTPClientInterface {
    private let urlSession: URLSessionInterface
    
    init(urlSession: URLSessionInterface) {
        self.urlSession = urlSession
    }
    
    func send(_ request: HTTPRequest, completion: @escaping (Result<HTTPResponse, Swift.Error>) -> Void) {
        do {
            let urlRequest = try makeURLRequest(from: request)
            
            urlSession.dataTask(with: urlRequest) { (data, response, error) in
                if let error = error {
                    return completion(.failure(error))
                }
                
                guard let response = response as? HTTPURLResponse else {
                    return completion(.failure(Error.invalidResponse(response)))
                }
                
                completion(.success(.init(statusCode: response.statusCode, data: data)))
            }.resume()
        } catch {
            completion(.failure(error))
        }
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
            urlComponents?.queryItems = !propertyList.isEmpty ? propertyList.map { URLQueryItem(name: $0.0, value: $0.1.description) } : nil

            switch request.method {
            case .get:
                urlRequest.url = urlComponents?.url
            case .post:
                urlRequest.httpBody = urlComponents?.percentEncodedQuery?.data(using: .utf8, allowLossyConversion: false)
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
