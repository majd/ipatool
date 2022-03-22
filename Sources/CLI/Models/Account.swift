//
//  Account.swift
//  IPATool
//
//  Created by Majd Alfhaily on 21.03.22.
//

import Foundation

struct Account: Codable {
    let name: String
    let email: String
    let passwordToken: String
    let directoryServicesIdentifier: String
}
