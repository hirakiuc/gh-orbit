import Foundation

struct EngineRuntimeConfiguration: Equatable {
    let baseRuntimeDirectory: String
    let socketPath: String
    let environment: [String: String]

    init(
        environment: [String: String] = ProcessInfo.processInfo.environment,
        homeDirectory: String = FileManager.default.homeDirectoryForCurrentUser.path
    ) {
        let baseRuntimeDirectory = environment["XDG_RUNTIME_DIR"] ?? homeDirectory + "/.local/run"

        self.baseRuntimeDirectory = baseRuntimeDirectory
        self.socketPath = baseRuntimeDirectory + "/gh-orbit/engine.sock"

        var environment = environment
        environment["XDG_RUNTIME_DIR"] = baseRuntimeDirectory
        self.environment = environment
    }
}
