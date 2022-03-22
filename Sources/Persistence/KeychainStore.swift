//
//  KeychainStore.swift
//  Persistence
//  
//  Created by Majd Alfhaily on 21.03.22.
//

import Foundation
import KeychainAccess

public protocol KeychainStoreInterface {
    func value<T: Codable>(forKey itemKey: String) throws -> T?
    func setValue<T: Codable>(_ item: T, forKey itemKey: String) throws
    func remove(_ key: String) throws
}

public class KeychainStore: KeychainStoreInterface {
    private let keychain: Keychain

    public init(service: String) {
        self.keychain = Keychain(service: service)
    }

    public func value<T: Codable>(forKey itemKey: String) throws -> T? {
        guard let data = try keychain.getData(itemKey) else {
            return nil
        }

        return try JSONDecoder().decode(T.self, from: data)
    }

    public func setValue<T: Codable>(_ item: T, forKey itemKey: String) throws {
        let data = try JSONEncoder().encode(item)
        try keychain.set(data, key: itemKey)
    }

    public func remove(_ key: String) throws {
        try keychain.remove(key)
    }
}
