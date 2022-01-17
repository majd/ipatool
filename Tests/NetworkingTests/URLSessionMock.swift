//
//  URLSessionMock.swift
//  NetworkingTests
//
//  Created by Majd Alfhaily on 17.01.22.
//

import Foundation
import Networking

final class URLSessionMock: URLSessionInterface {
    var onData: ((_ request: URLRequest) async throws -> (Data, URLResponse))?

    func data(for request: URLRequest) async throws -> (Data, URLResponse) {
        guard let onData = onData else {
            fatalError("Override implementation using `onData`.")
        }
        
        return try await onData(request)
    }
}
