//
//  main.swift
//  SwiftLint
//
//  Created by Majd Alfhaily on 15.06.22.
//

import PackagePlugin
import Foundation

@main
struct SwiftLint: BuildToolPlugin {
    func createBuildCommands(context: PluginContext, target: Target) async throws -> [Command] {
        let toolPath = try determinePath(forTool: "swiftlint")
        let configPath = context.package.directory.appending(".swiftlint.yml")

        return [
            .buildCommand(
                displayName: "Run SwiftLint",
                executable: toolPath,
                arguments: [
                    "lint",
                    "--in-process-sourcekit",
                    "--config",
                    configPath.string,
                    "--path",
                    target.directory.string
                ]
            )
        ]
    }

    private func determinePath(forTool toolName: String) throws -> Path {
        let prefixes: [String] = [
            "/opt/homebrew/bin",
            "/usr/local/bin",
            "/usr/bin",
            "/bin"
        ]

        for path in prefixes where FileManager.default.fileExists(atPath: "\(path)/\(toolName)") {
            return Path("\(path)/\(toolName)")
        }

        throw NSError(
            domain: Bundle.main.bundleIdentifier ?? String(describing: type(of: self)),
            code: 0,
            userInfo: [NSLocalizedDescriptionKey: "Could not find tool: \(toolName)."]
        )
    }
}
