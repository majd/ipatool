//
//  HTTPDownloadClient.swift
//  Networking
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

public protocol HTTPDownloadClientInterface {
    func download(from source: URL, to target: URL, progress: @escaping (Float) -> Void) throws
}

public final class HTTPDownloadClient: NSObject, HTTPDownloadClientInterface {
    private var session: URLSession!
    private var progressHandler: ((Float) -> Void)?
    private var semaphore: DispatchSemaphore?
    private var targetURL: URL?
    private var result: Result<Void, Swift.Error>?

    public override init() {
        super.init()
        self.session = URLSession(configuration: .default, delegate: self, delegateQueue: nil)
    }
    
    public func download(from source: URL, to target: URL, progress: @escaping (Float) -> Void) throws {
        assert(progressHandler == nil)
        assert(semaphore == nil)
        assert(targetURL == nil)

        progressHandler = progress
        targetURL = target

        semaphore = DispatchSemaphore(value: 0)
        session.downloadTask(with: source).resume()
        semaphore?.wait()

        switch result {
        case let .failure(error):
            throw error
        case .success:
            break
        case .none:
            preconditionFailure()
        }
    }
}

extension HTTPDownloadClient: URLSessionDownloadDelegate {
    public func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Swift.Error?) {
        guard let error = error else {
            return
        }

        defer {
            semaphore?.signal()
            progressHandler = nil
            semaphore = nil
            targetURL = nil
        }

        result = .failure(error)
    }
    
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
            semaphore?.signal()
            progressHandler = nil
            semaphore = nil
            targetURL = nil
        }
        
        guard let target = targetURL else {
            result = .failure(Error.invalidTarget)
            return
        }
        
        do {
            try FileManager.default.moveItem(at: location, to: target)
            result = .success(())
        } catch {
            result = .failure(error)
        }
    }
}

extension HTTPDownloadClient {
    enum Error: Swift.Error {
        case invalidTarget
    }
}
