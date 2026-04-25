import Foundation

/// PathResolver handles the discovery of the gh-orbit binary across different environments.
struct PathResolver {
    static func resolveBinary() -> URL? {
        let fileManager = FileManager.default

        // 1. GH_ORBIT_BIN environment override
        if let envPath = ProcessInfo.processInfo.environment["GH_ORBIT_BIN"],
            !envPath.isEmpty
        {
            let url = URL(fileURLWithPath: envPath)
            if fileManager.fileExists(atPath: url.path) {
                return url
            }
        }

        // 2. App Bundle (Production)
        if let bundleURL = Bundle.main.url(forAuxiliaryExecutable: "gh-orbit") {
            return bundleURL
        }

        // 3. Project Root (Debug/Development only)
        #if DEBUG
            if let devURL = resolveDevBinary() {
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
            if fileManager.fileExists(atPath: url.path) {
                return url
            }
        }

        return nil
    }

    #if DEBUG
        private static func resolveDevBinary() -> URL? {
            let fileManager = FileManager.default
            let currentDir = fileManager.currentDirectoryPath
            var searchURL = URL(fileURLWithPath: currentDir)

            // Walk up at most 5 levels to find project root (containing bin/gh-orbit)
            for _ in 0..<5 {
                let binURL = searchURL.appendingPathComponent("bin/gh-orbit")
                if fileManager.fileExists(atPath: binURL.path) {
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
                if fileManager.fileExists(atPath: goModURL.path) || fileManager.fileExists(atPath: agentsURL.path) {
                    break
                }

                searchURL.deleteLastPathComponent()
            }

            return nil
        }
    #endif
}
