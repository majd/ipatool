//
//  HTTPDownloadClient.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

protocol HTTPDownloadClientInterface {
    func download(from source: URL, to target: URL, progress: @escaping (Float) -> Void, completion: @escaping (Result<Void, Error>) -> Void)
}

extension HTTPDownloadClientInterface {
    func download(from source: URL, to target: URL, progress: @escaping (Float) -> Void) throws {
        let semaphore = DispatchSemaphore(value: 0)
        var result: Result<Void, Error>?
        
        download(from: source, to: target, progress: progress) {
            result = $0
            semaphore.signal()
        }
        
        _ = semaphore.wait(timeout: .distantFuture)
        
        switch result {
        case .none:
            throw HTTPClient.Error.timeout
        case let .failure(error):
            throw error
        default:
            break
        }
    }
}

final class HTTPDownloadClient: NSObject, HTTPDownloadClientInterface {
    private var urlSession: URLSession!
    private var progressHandler: ((Float) -> Void)?
    private var completionHandler: ((Result<Void, Swift.Error>) -> Void)?
    private var targetURL: URL?

    override init() {
        super.init()
        self.urlSession = URLSession(configuration: .default, delegate: self, delegateQueue: nil)
    }
    
    func download(from source: URL, to target: URL, progress: @escaping (Float) -> Void, completion: @escaping (Result<Void, Swift.Error>) -> Void) {
        assert(progressHandler == nil)
        assert(completionHandler == nil)
        assert(targetURL == nil)

        progressHandler = progress
        completionHandler = completion
        targetURL = target
        urlSession.downloadTask(with: source).resume()
    }
}

extension HTTPDownloadClient: URLSessionDownloadDelegate {
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didWriteData bytesWritten: Int64, totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        progressHandler?(Float(totalBytesWritten) / Float(totalBytesExpectedToWrite))
    }
    
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {
        defer {
            progressHandler = nil
            completionHandler = nil
            targetURL = nil
        }
        
        guard let target = targetURL else {
            return completionHandler?(.failure(Error.invalidTarget)) ?? ()
        }
        
        do {
            try FileManager.default.moveItem(at: location, to: target)
            completionHandler?(.success(()))
        } catch {
            completionHandler?(.failure(error))
        }
    }
}

extension HTTPDownloadClient {
    enum Error: Swift.Error {
        case invalidTarget
    }
}
