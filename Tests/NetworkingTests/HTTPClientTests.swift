//
//  HTTPClientTests.swift
//  NetworkingTests
//
//  Created by Majd Alfhaily on 17.01.22.
//

import XCTest
@testable import Networking

final class HTTPClientTests: XCTestCase {
    private var sut: HTTPClient!
    private var session: URLSessionMock!
    
    override func setUp() {
        session = URLSessionMock()
        sut = HTTPClient(session: session)
    }
    
    func test_GET_success_returnsValidResposne() throws {
        session.onDataTask = { request in
            let url = try XCTUnwrap(request.url?.absoluteString)
            XCTAssertTrue(url.hasPrefix("https://api.example.com"))
            XCTAssertEqual(request.httpMethod, "GET")
            
            let data = try XCTUnwrap("foo".data(using: .utf8))
            let response = HTTPURLResponse(
                url: URL(string: "https://example.com")!,
                mimeType: nil,
                expectedContentLength: 0,
                textEncodingName: nil
            )
            return (data, response)
        }
        
        let response = try sut.send(TestRequest.get(nil))
        let data = try XCTUnwrap(response.data)
        XCTAssertEqual(String(data: data, encoding: .utf8), "foo")
    }

    func test_GET_failure_returnsInvalidResposne() throws {
        session.onDataTask = { request in
            let url = try XCTUnwrap(request.url?.absoluteString)
            XCTAssertTrue(url.hasPrefix("https://api.example.com"))
            XCTAssertEqual(request.httpMethod, "GET")
            
            let data = try XCTUnwrap("foo".data(using: .utf8))
            
            let response = URLResponse(
                url: URL(string: "https://example.com")!,
                mimeType: nil,
                expectedContentLength: 0,
                textEncodingName: nil
            )
            return (data, response)
        }

        do {
            _ = try sut.send(TestRequest.get(nil))
            XCTFail()
        } catch {
            XCTAssertNotNil(error)
        }
    }
    
    func test_POST_xmlEncoding_returnsValidResponse() throws {
        session.onDataTask = { request in
            let url = try XCTUnwrap(request.url?.absoluteString)
            XCTAssertTrue(url.hasPrefix("https://api.example.com"))
            XCTAssertEqual(request.httpMethod, "POST")

            let data = try XCTUnwrap(request.httpBody)
            let decoded = try PropertyListSerialization.propertyList(
                from: data,
                options: [],
                format: nil
            ) as! [String: String]
            
            XCTAssertEqual(decoded["foo"], "bar")

            let response = HTTPURLResponse(
                url: URL(string: "https://example.com")!,
                mimeType: nil,
                expectedContentLength: 0,
                textEncodingName: nil
            )
            return (data, response)
        }
        
        _ = try sut.send(TestRequest.post(.xml(["foo": "bar"])))
    }
    
    func test_GET_urlEncoding_returnsValidResponse() throws {
        session.onDataTask = { request in
            let url = try XCTUnwrap(request.url?.absoluteString)
            XCTAssertTrue(url.hasPrefix("https://api.example.com"))
            XCTAssertTrue(url.hasSuffix("?foo=bar"))
            XCTAssertEqual(request.httpMethod, "GET")

            let response = HTTPURLResponse(
                url: URL(string: "https://example.com")!,
                mimeType: nil,
                expectedContentLength: 0,
                textEncodingName: nil
            )
            return (Data(), response)
        }
        
        _ = try sut.send(TestRequest.get(.urlEncoding(["foo": "bar"])))
    }
    
    func test_POST_urlEncoding_returnsValidResponse() throws {
        session.onDataTask = { request in
            let url = try XCTUnwrap(request.url?.absoluteString)
            let data = try XCTUnwrap(request.httpBody)
            let decoded = String(data: data, encoding: .utf8)
            let headerValue = try XCTUnwrap(request.allHTTPHeaderFields?["X-Test"])
            
            XCTAssertTrue(url.hasPrefix("https://api.example.com"))
            XCTAssertEqual(decoded, "foo=bar")
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(headerValue, "true")

            let response = HTTPURLResponse(
                url: URL(string: "https://example.com")!,
                mimeType: nil,
                expectedContentLength: 0,
                textEncodingName: nil
            )
            return (Data(), response)
        }
        
        _ = try sut.send(TestRequest.post(.urlEncoding(["foo": "bar"])))
    }
}

private enum TestRequest: HTTPRequest {
    case get(HTTPPayload?)
    case post(HTTPPayload)
    
    var method: HTTPMethod {
        switch self {
        case .get:
            return .get
        case .post:
            return .post
        }
    }
    
    var endpoint: HTTPEndpoint {
        switch self {
        case .get, .post:
            return TestEndpoint.generic
        }
    }
    
    var payload: HTTPPayload? {
        switch self {
        case let .get(payload):
            return payload
        case let .post(payload):
            return payload
        }
    }
    
    var headers: [String: String] {
        switch self {
        case .get, .post:
            return ["X-Test": "true"]
        }
    }
}

private enum TestEndpoint: HTTPEndpoint {
    case generic
    
    var url: URL {
        switch self {
        case .generic:
            return URL(string: "https://api.example.com/\(UUID().uuidString)")!
        }
    }
}
