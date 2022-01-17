//
//  HTTPDownloadClient.swift
//  Networking
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

public protocol HTTPDownloadClientInterface {
    func download(from source: URL, to target: URL, progress: @escaping (Float) -> Void) async throws
}

public final class HTTPDownloadClient: NSObject, HTTPDownloadClientInterface {
    private var session: URLSession!
    private var progressHandler: ((Float) -> Void)?
    private var continuation: CheckedContinuation<Void, Swift.Error>?
    private var targetURL: URL?

    public override init() {
        super.init()
        self.session = URLSession(configuration: .default, delegate: self, delegateQueue: nil)
    }
    
    public func download(from source: URL, to target: URL, progress: @escaping (Float) -> Void) async throws {
        assert(progressHandler == nil)
        assert(continuation == nil)
        assert(targetURL == nil)

        progressHandler = progress
        targetURL = target

        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Swift.Error>) in
            self.continuation = continuation
            let task = session.downloadTask(with: source)
            task.resume()
        }
    }
}

extension HTTPDownloadClient: URLSessionDownloadDelegate {
    public func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didWriteData bytesWritten: Int64,
        totalBytesWritten: Int64,
        totalBytesExpectedToWrite: Int64
    ) {
        progressHandler?(Float(totalBytesWritten) / Float(totalBytesExpectedToWrite))
    }
    
    public func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didFinishDownloadingTo location: URL
    ) {
        defer {
            progressHandler = nil
            continuation = nil
            targetURL = nil
        }
        
        guard let target = targetURL else {
            return continuation?.resume(throwing: Error.invalidTarget) ?? ()
        }
        
        do {
            try FileManager.default.moveItem(at: location, to: target)
            continuation?.resume()
        } catch {
            continuation?.resume(throwing: error)
        }
    }
}

extension HTTPDownloadClient {
    enum Error: Swift.Error {
        case invalidTarget
    }
}
