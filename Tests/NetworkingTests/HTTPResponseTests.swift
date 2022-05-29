//
//  HTTPResponseTests.swift
//  NetworkingTests
//
//  Created by Majd Alfhaily on 17.01.22.
//

import XCTest
@testable import Networking

final class HTTPResponseTests: XCTestCase {
    func test_decode_jsonData_returnsObject() throws {
        let data = try JSONEncoder().encode(["foo": "bar"])
        let response = HTTPResponse(statusCode: 200, data: data)
        
        let object = try response.decode([String: String].self, as: .json)
        XCTAssertEqual(object["foo"], "bar")
    }

    func test_decode_xmlData_returnsObject() throws {
        let data = try PropertyListEncoder().encode(["foo": "bar"])
        let response = HTTPResponse(statusCode: 200, data: data)
        
        let object = try response.decode([String: String].self, as: .xml)
        XCTAssertEqual(object["foo"], "bar")
    }

    func test_decode_noData_returnsObject() throws {
        let response = HTTPResponse(statusCode: 200, data: nil)
        XCTAssertThrowsError(try response.decode([String: String].self, as: .xml))
    }

    func test_decode_invalidData_returnsObject() throws {
        let data = try PropertyListEncoder().encode(["foo": "bar"])
        let response = HTTPResponse(statusCode: 200, data: data)

        XCTAssertThrowsError(try response.decode([String: String].self, as: .json))
    }
}
