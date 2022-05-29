//
//  URLSessionDataTaskMock.swift
//  NetworkingTests
//
//  Created by Majd Alfhaily on 29.05.22.
//

import Foundation
import Networking

final class URLSessionDataTaskMock: URLSessionDataTaskInterface {
    var onResume: (() -> Void)?

    func resume() {
        guard let onResume = onResume else {
            fatalError("Override implementation using `onResume`.")
        }

        onResume()
    }
}
