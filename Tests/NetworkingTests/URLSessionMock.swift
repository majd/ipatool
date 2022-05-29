//
//  URLSessionMock.swift
//  NetworkingTests
//
//  Created by Majd Alfhaily on 17.01.22.
//

import Foundation
import Networking

final class URLSessionMock: URLSessionInterface {
    var onDataTask: ((URLRequest) throws -> (data: Data?, response: URLResponse?))?

    func dataTask(
        with request: URLRequest,
        completionHandler: @escaping (Data?, URLResponse?, Error?) -> Void
    ) -> URLSessionDataTaskInterface {
        guard let onDataTask = onDataTask else {
            fatalError("Override implementation using `onDataTask`.")
        }

        let dataTask = URLSessionDataTaskMock()

        dataTask.onResume = {
            do {
                let result = try onDataTask(request)
                completionHandler(result.data, result.response, nil)
            } catch {
                completionHandler(nil, nil, error)
            }
        }

        return dataTask
    }
}
