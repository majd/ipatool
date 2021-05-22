//
//  StoreResponse.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

enum StoreResponse {
    case failure(error: Swift.Error)
    case account(Account)
    case item(Item)
}

extension StoreResponse {
    struct Account {
        let firstName: String
        let lastName: String
        let directoryServicesIdentifier: String
    }
    
    struct Item {
        let url: URL
        let md5: String
        let signatures: [Signature]
        let metadata: [String: Any]
    }

    enum Error: Int, Swift.Error {
        case unknownError = 0
        case genericError = 5002
        case codeRequired = 1
        case invalidLicense = 9610
        case invalidCredentials = -5000
        case invalidAccount = 5001
        case invalidItem = -10000
        case lockedAccount = -10001
    }
}

extension StoreResponse: Decodable {
    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)

        let error = try container.decodeIfPresent(String.self, forKey: .error)
        let message = try container.decodeIfPresent(String.self, forKey: .message)

        if container.contains(.account) {
            let directoryServicesIdentifier = try container.decode(String.self, forKey: .directoryServicesIdentifier)
            let accountContainer = try container.nestedContainer(keyedBy: AccountInfoCodingKeys.self, forKey: .account)
            let addressContainer = try accountContainer.nestedContainer(keyedBy: AddressCodingKeys.self, forKey: .address)
            let firstName = try addressContainer.decode(String.self, forKey: .firstName)
            let lastName = try addressContainer.decode(String.self, forKey: .lastName)
            
            self = .account(.init(firstName: firstName, lastName: lastName, directoryServicesIdentifier: directoryServicesIdentifier))
        } else if let items = try container.decodeIfPresent([Item].self, forKey: .items), let item = items.first {
            self = .item(item)
        } else if let error = error, !error.isEmpty {
            self = .failure(error: Error(rawValue: Int(error) ?? 0) ?? .unknownError)
        } else {
            switch message {
            case "Your account information was entered incorrectly.":
                self = .failure(error: Error.invalidCredentials)
            case "An Apple ID verification code is required to sign in. Type your password followed by the verification code shown on your other devices.":
                self = .failure(error: Error.codeRequired)
            case "This Apple ID has been locked for security reasons. Visit iForgot to reset your account (https://iforgot.apple.com).":
                self = .failure(error: Error.lockedAccount)
            default:
                self = .failure(error: Error.unknownError)
            }
        }
    }
    
    private enum CodingKeys: String, CodingKey {
        case directoryServicesIdentifier = "dsPersonId"
        case message = "customerMessage"
        case items = "songList"
        case error = "failureType"
        case account = "accountInfo"
    }
    
    private enum AccountInfoCodingKeys: String, CodingKey {
        case address
    }

    private enum AddressCodingKeys: String, CodingKey {
        case firstName
        case lastName
    }
}

extension StoreResponse.Item: Decodable {
    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let md5 = try container.decode(String.self, forKey: .md5)

        guard let key = CodingUserInfoKey(rawValue: "data"),
              let data = decoder.userInfo[key] as? Data,
              let json = try PropertyListSerialization.propertyList(from: data, options: [], format: nil) as? [String: Any],
              let items = json["songList"] as? [[String: Any]],
              let item = items.first(where: { $0["md5"] as? String == md5 }),
              let metadata = item["metadata"] as? [String: Any]
        else { throw StoreResponse.Error.invalidItem }

        let absoluteUrl = try container.decode(String.self, forKey: .url)

        self.md5 = md5
        self.metadata = metadata
        self.signatures = try container.decode([Signature].self, forKey: .signatures)

        if let url = URL(string: absoluteUrl) {
            self.url = url
        } else {
            let context = DecodingError.Context(codingPath: [CodingKeys.url], debugDescription: "URL contains illegal characters: \(absoluteUrl).")
            throw DecodingError.keyNotFound(CodingKeys.url, context)
        }
    }
    
    struct Signature: Decodable {
        let id: Int
        let sinf: Data
    }
    
    enum CodingKeys: String, CodingKey {
        case url = "URL"
        case metadata
        case md5
        case signatures = "sinfs"
    }
}
