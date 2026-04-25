import Foundation

/// Protocol for file system operations to allow mocking.
protocol FileSystem {
    func fileExists(atPath: String) -> Bool
    var currentDirectoryPath: String { get }
}

struct RealFileSystem: FileSystem {
    func fileExists(atPath: String) -> Bool {
        return FileManager.default.fileExists(atPath: atPath)
    }
    var currentDirectoryPath: String {
        return FileManager.default.currentDirectoryPath
    }
}

/// PathResolver handles the discovery of the gh-orbit binary across different environments.
struct PathResolver {
    static func resolveBinary(
        fileSystem: FileSystem = RealFileSystem(),
        env: [String: String] = ProcessInfo.processInfo.environment
    ) -> URL? {

        // 1. GH_ORBIT_BIN environment override
        if let envPath = env["GH_ORBIT_BIN"], !envPath.isEmpty {
            let url = URL(fileURLWithPath: envPath)
            if fileSystem.fileExists(atPath: url.path) {
                return url
            }
        }

        // 2. App Bundle (Production)
        if let bundleURL = Bundle.main.url(forAuxiliaryExecutable: "gh-orbit") {
            return bundleURL
        }

        // 3. Project Root (Debug/Development only)
        #if DEBUG
            if let devURL = resolveDevBinary(fileSystem: fileSystem) {
                return devURL
            }
        #endif

        // 4. Standard Absolute Fallbacks
        let fallbacks = [
            "/usr/local/bin/gh-orbit",
            "/opt/homebrew/bin/gh-orbit",
        ]
        for path in fallbacks {
            let url = URL(fileURLWithPath: path)
            if fileSystem.fileExists(atPath: url.path) {
                return url
            }
        }

        return nil
    }

    #if DEBUG
        private static func resolveDevBinary(fileSystem: FileSystem) -> URL? {
            let currentDir = fileSystem.currentDirectoryPath
            var searchURL = URL(fileURLWithPath: currentDir)

            // Walk up at most 5 levels to find project root (containing bin/gh-orbit)
            for _ in 0..<5 {
                let binURL = searchURL.appendingPathComponent("bin/gh-orbit")
                if fileSystem.fileExists(atPath: binURL.path) {
                    return binURL
                }

                // Look for project sentinels
                let goModURL = searchURL.appendingPathComponent("go.mod")
                let agentsURL = searchURL.appendingPathComponent("AGENTS.md")

                // Stop if we hit user home or root or find project markers
                if searchURL.path == "/" || searchURL.path == "/Users" {
                    break
                }

                // If we find markers but bin/gh-orbit wasn't there, stop anyway
                if fileSystem.fileExists(atPath: goModURL.path) || fileSystem.fileExists(atPath: agentsURL.path) {
                    break
                }

                searchURL.deleteLastPathComponent()
            }

            return nil
        }
    #endif
}
