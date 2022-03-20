//
//  StoreResponse.swift
//  StoreAPI
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation

public enum StoreResponse {
    case failure(error: Swift.Error)
    case account(Account)
    case item(Item)
    case buyReceipt(BuyReceipt)
}

extension StoreResponse {
    public struct Account {
        public let firstName: String
        public let lastName: String
        public let directoryServicesIdentifier: String
        public let passwordToken: String
    }
    
    public struct Item {
        public let url: URL
        public let md5: String
        public let signatures: [Signature]
        public let metadata: [String: Any]
    }
    
    public struct BuyReceipt {
        public let statusCode: Int
        public let statusType: String
    }

    public enum Error: Int, Swift.Error {
        case unknownError = 0
        case genericError = 5002
        case codeRequired = 1
        case invalidLicense = 9610
        case invalidCredentials = -5000
        case invalidAccount = 5001
        case invalidItem = -10000
        case lockedAccount = -10001
        case wrongCountry = -128
    }
}

extension StoreResponse: Decodable {
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)

        let error = try container.decodeIfPresent(String.self, forKey: .error)
        let message = try container.decodeIfPresent(String.self, forKey: .message)

        if container.contains(.account) {
            let directoryServicesIdentifier = try container.decode(String.self, forKey: .directoryServicesIdentifier)
            let passwordToken = try container.decode(String.self, forKey: .passwordToken)
            let accountContainer = try container.nestedContainer(keyedBy: AccountInfoCodingKeys.self, forKey: .account)
            let addressContainer = try accountContainer.nestedContainer(keyedBy: AddressCodingKeys.self, forKey: .address)
            let firstName = try addressContainer.decode(String.self, forKey: .firstName)
            let lastName = try addressContainer.decode(String.self, forKey: .lastName)
            
            self = .account(.init(firstName: firstName, lastName: lastName, directoryServicesIdentifier: directoryServicesIdentifier, passwordToken: passwordToken))
        } else if let items = try container.decodeIfPresent([Item].self, forKey: .items), let item = items.first {
            self = .item(item)
        } else if container.contains(.statusCode) {
            let statusCode = try container.decode(Int.self, forKey: .statusCode)
            let statusType = try container.decode(String.self, forKey: .statusType)
            
            self = .buyReceipt(.init(statusCode: statusCode, statusType: statusType))
        } else if let error = error, !error.isEmpty {
            self = .failure(error: Error(rawValue: Int(error) ?? 0) ?? .unknownError)
        } else {
            switch message {
            case "Your account information was entered incorrectly.":
                self = .failure(error: Error.invalidCredentials)
            case "An Apple ID verification code is required to sign in. Type your password followed by the verification code shown on your other devices.":
                self = .failure(error: Error.codeRequired)
            case "MZFinance.BadLogin.Configurator_message":
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
        case passwordToken = "passwordToken"
        case message = "customerMessage"
        case items = "songList"
        case error = "failureType"
        case account = "accountInfo"
        case statusCode = "status"
        case statusType = "jingleDocType"
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
    public init(from decoder: Decoder) throws {
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
    
    public struct Signature: Decodable {
        public let id: Int
        public let sinf: Data
    }
    
    enum CodingKeys: String, CodingKey {
        case url = "URL"
        case metadata
        case md5
        case signatures = "sinfs"
    }
}
