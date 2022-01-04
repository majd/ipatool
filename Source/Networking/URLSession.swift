//
//  URLSession.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

protocol URLSessionInterface {
    func data(for request: URLRequest) async throws -> (Data, URLResponse)
}

extension URLSession: URLSessionInterface {
    func data(for request: URLRequest) async throws -> (Data, URLResponse) {
        try await withCheckedThrowingContinuation { continuation in
            let task = dataTask(with: request) { data, response, error in
                guard let data = data, let response = response else {
                    let error = error ?? URLError(.badServerResponse)
                    return continuation.resume(throwing: error)
                }

                continuation.resume(returning: (data, response))
            }

            task.resume()
        }
    }
}
