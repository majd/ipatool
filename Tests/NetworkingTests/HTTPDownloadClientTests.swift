//
//  HTTPDownloadClient.swift
//  NetworkingTests
//
//  Created by Majd Alfhaily on 17.01.22.
//

import XCTest
@testable import Networking

final class HTTPDownloadClientTests: XCTestCase {
    private var sut: HTTPDownloadClient!
    
    override func setUp() {
        sut = HTTPDownloadClient()
    }
    
    func test_download_success_returnsValidResponse() throws {
        let source = try XCTUnwrap(URL(string: "https://proof.ovh.net/files/1Mb.dat"))
        let target = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent(UUID().uuidString)
        var lastValue: Float = 0

        try sut.download(from: source, to: target) { value in
            XCTAssertGreaterThanOrEqual(value, lastValue)
            lastValue = value
        }
        
        XCTAssertTrue(FileManager.default.fileExists(atPath: target.path))
    }
    
    func test_download_failure_throwsError() throws {
        let source = try XCTUnwrap(URL(string: "https://\(UUID().uuidString).test"))
        let target = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent(UUID().uuidString)

        do {
            try sut.download(from: source, to: target) { _ in }
        } catch {
            XCTAssertNotNil(error)
        }
    }
}
