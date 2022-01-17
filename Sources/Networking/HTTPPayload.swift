//
//  HTTPPayload.swift
//  Networking
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

public enum HTTPPayload {
    case xml([String: String])
    case urlEncoding([String: String])
}
