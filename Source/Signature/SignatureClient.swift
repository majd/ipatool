//
//  SignatureClient.swift
//  IPATool
//
//  Created by Majd Alfhaily on 22.05.21.
//

import Foundation
import ZIPFoundation

protocol SignatureClientInterface {
    func appendMetadata(item: StoreResponse.Item, email: String) throws
    func appendSignature(item: StoreResponse.Item) throws
}

final class SignatureClient: SignatureClientInterface {
    private let fileManager: FileManager
    private let filePath: String
    
    init(fileManager: FileManager, filePath: String) {
        self.fileManager = fileManager
        self.filePath = filePath
    }
    
    func appendMetadata(item: StoreResponse.Item, email: String) throws {
        guard let archive = Archive(url: URL(fileURLWithPath: filePath), accessMode: .update) else  {
            throw Error.invalidArchive
        }

        var metadata = item.metadata
        metadata["apple-id"] = email
        metadata["userName"] = email

        let metadataUrl = URL(fileURLWithPath: NSTemporaryDirectory().appending("\(UUID().uuidString)/iTunesMetadata.plist"))

        try fileManager.createDirectory(at: metadataUrl.deletingLastPathComponent(), withIntermediateDirectories: true)
        try PropertyListSerialization.data(fromPropertyList: metadata, format: .xml, options: .zero).write(to: metadataUrl)
        try archive.addEntry(with: metadataUrl.lastPathComponent, relativeTo: metadataUrl.deletingLastPathComponent())
        try fileManager.removeItem(at: metadataUrl)
    }
    
    func appendSignature(item: StoreResponse.Item) throws {
        guard let archive = Archive(url: URL(fileURLWithPath: filePath), accessMode: .update) else  {
            throw Error.invalidArchive
        }

        let manifest = try readPlist(archive: archive, matchingSuffix: ".app/SC_Info/Manifest.plist", type: Manifest.self)

        guard let infoEntry = archive.first(where: { $0.path.hasSuffix(".app/Info.plist") }) else {
            throw Error.invalidAppBundle
        }
        
        let appBundleName = URL(fileURLWithPath: infoEntry.path)
            .deletingLastPathComponent()
            .deletingPathExtension()
            .lastPathComponent

        guard let signatureItem = item.signatures.first(where: { $0.id == 0 }), let signatureTargetPath = manifest.paths.first else {
            throw Error.invalidSignature
        }

        let signatureBaseUrl = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent(UUID().uuidString)
        let signatureUrl = signatureBaseUrl
            .appendingPathComponent("Payload")
            .appendingPathComponent(appBundleName)
            .appendingPathExtension("app")
            .appendingPathComponent(signatureTargetPath)
        
        let signatureRelativePath = signatureUrl.path.replacingOccurrences(of: "\(signatureBaseUrl.path)/", with: "")

        try fileManager.createDirectory(at: signatureUrl.deletingLastPathComponent(), withIntermediateDirectories: true)
        try signatureItem.sinf.write(to: signatureUrl)
        try archive.addEntry(with: signatureRelativePath, relativeTo: signatureBaseUrl)
        try fileManager.removeItem(at: signatureBaseUrl)
    }
    
    private func readPlist<T: Decodable>(archive: Archive, matchingSuffix: String, type: T.Type) throws -> T {
        guard let entry = archive.first(where: { $0.path.hasSuffix(matchingSuffix) }) else {
            throw Error.fileNotFound(matchingSuffix)
        }
        
        let url = URL(fileURLWithPath: NSHomeDirectory()).appendingPathComponent(UUID().uuidString).appendingPathExtension("plist")
        _ = try archive.extract(entry, to: url)

        let data = try Data(contentsOf: url)
        let plist = try PropertyListDecoder().decode(type, from: data)

        try FileManager.default.removeItem(at: url)

        return plist
    }
}

extension SignatureClient {
    struct Manifest: Codable {
        let paths: [String]
        
        enum CodingKeys: String, CodingKey {
            case paths = "SinfPaths"
        }
    }

    enum Error: Swift.Error {
        case invalidArchive
        case invalidAppBundle
        case invalidSignature
        case fileNotFound(String)
    }
}
