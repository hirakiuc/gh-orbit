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
        env: [String: String] = ProcessInfo.processInfo.environment,
        onLog: ((String, LogLevel) -> Void)? = nil
    ) -> URL? {
        onLog?("Starting binary resolution...", .debug)

        // 1. App Bundle (Production Highest Priority - Prevents Hijacking)
        if let bundleURL = Bundle.main.url(forAuxiliaryExecutable: "gh-orbit") {
            onLog?("Found binary via Bundle.main at: \(bundleURL.path)", .debug)
            return bundleURL
        }

        // 2. Manual Bundle Lookup Fallback (Defensive)
        if let executableURL = Bundle.main.executableURL {
            let helpersURL =
                executableURL
                .deletingLastPathComponent()  // MacOS
                .deletingLastPathComponent()  // Contents
                .appendingPathComponent("Helpers/gh-orbit")
            onLog?("Checking manual fallback at: \(helpersURL.path)", .debug)
            if fileSystem.fileExists(atPath: helpersURL.path) {
                onLog?("Found binary via manual fallback at: \(helpersURL.path)", .debug)
                return helpersURL
            }
        }

        // 3. GH_ORBIT_BIN environment override (Debug/Development Only)
        #if DEBUG
            if let envPath = env["GH_ORBIT_BIN"], !envPath.isEmpty {
                let url = URL(fileURLWithPath: envPath)
                onLog?("Checking GH_ORBIT_BIN at: \(url.path)", .debug)
                if fileSystem.fileExists(atPath: url.path) {
                    onLog?("Found binary via GH_ORBIT_BIN at: \(url.path)", .debug)
                    return url
                }
            }
        #endif

        // 4. Project Root (Debug/Development Only)
        #if DEBUG
            if let devURL = resolveDevBinary(fileSystem: fileSystem, onLog: onLog) {
                return devURL
            }
        #endif

        // 5. Standard Absolute Fallbacks
        let fallbacks = [
            "/usr/local/bin/gh-orbit",
            "/opt/homebrew/bin/gh-orbit"
        ]
        for path in fallbacks {
            let url = URL(fileURLWithPath: path)
            onLog?("Checking absolute fallback at: \(url.path)", .debug)
            if fileSystem.fileExists(atPath: url.path) {
                onLog?("Found binary via fallback at: \(url.path)", .debug)
                return url
            }
        }

        onLog?("Failed to find gh-orbit binary.", .error)
        return nil
    }

    #if DEBUG
        private static func resolveDevBinary(fileSystem: FileSystem, onLog: ((String, LogLevel) -> Void)?) -> URL? {
            let currentDir = fileSystem.currentDirectoryPath
            var searchURL = URL(fileURLWithPath: currentDir)
            onLog?("Starting project root walk-up from: \(currentDir)", .debug)

            for level in 0..<5 {
                let binURL = searchURL.appendingPathComponent("bin/gh-orbit")
                onLog?("Checking level \(level) at: \(binURL.path)", .debug)

                if fileSystem.fileExists(atPath: binURL.path) {
                    onLog?("Found binary via project root at: \(binURL.path)", .debug)
                    return binURL
                }

                let goModURL = searchURL.appendingPathComponent("go.mod")
                let agentsURL = searchURL.appendingPathComponent("AGENTS.md")

                if searchURL.path == "/" || searchURL.path == "/Users" {
                    onLog?("Hit system boundary at \(searchURL.path), stopping walk-up.", .debug)
                    break
                }

                if fileSystem.fileExists(atPath: goModURL.path) || fileSystem.fileExists(atPath: agentsURL.path) {
                    onLog?("Hit project sentinel at \(searchURL.path), stopping walk-up.", .debug)
                    break
                }

                searchURL.deleteLastPathComponent()
            }

            return nil
        }
    #endif
}
