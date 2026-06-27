import Foundation

struct EngineRuntimeConfiguration: Equatable {
    let baseRuntimeDirectory: String
    let socketPath: String
    let reviewWorkspaceRoot: String
    let environment: [String: String]

    init(
        environment: [String: String] = ProcessInfo.processInfo.environment,
        homeDirectory: String = FileManager.default.homeDirectoryForCurrentUser.path,
        applicationSupportDirectory: String? = nil,
        reviewWorkspaceRoot: String? = nil
    ) {
        let baseRuntimeDirectory = environment["XDG_RUNTIME_DIR"] ?? homeDirectory + "/.local/run"
        let appSupportDirectory =
            applicationSupportDirectory
            ?? FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first?.path
            ?? homeDirectory + "/Library/Application Support"

        self.baseRuntimeDirectory = baseRuntimeDirectory
        self.socketPath = baseRuntimeDirectory + "/gh-orbit/engine.sock"
        self.reviewWorkspaceRoot = reviewWorkspaceRoot ?? appSupportDirectory + "/gh-orbit/worktrees"

        var environment = environment
        environment["XDG_RUNTIME_DIR"] = baseRuntimeDirectory
        self.environment = environment
    }
}
