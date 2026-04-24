import Foundation

/// NativeEngineManager manages the persistent gh-orbit engine process.
@MainActor
class NativeEngineManager: ObservableObject {
    @Published var isEngineReady: Bool = false
    private var engineSupervisor = ProcessSupervisor()
    private var socketPath: String

    private let maxAttempts: Int
    private let baseDelayNS: UInt64

    init(socketPath: String? = nil, maxAttempts: Int = 10, baseDelayNS: UInt64 = 50_000_000) {
        self.maxAttempts = maxAttempts
        self.baseDelayNS = baseDelayNS

        if let socketPath = socketPath {
            self.socketPath = socketPath
        } else {
            // Resolve socket path
            let runtimeDir =
                ProcessInfo.processInfo.environment["XDG_RUNTIME_DIR"]
                ?? (FileManager.default.homeDirectoryForCurrentUser.path + "/.local/run/gh-orbit")
            self.socketPath = runtimeDir + "/engine.sock"
        }
    }

    func startEngine(executable: URL) async {
        guard !engineSupervisor.isRunning else { return }

        do {
            // Start engine with verbose logging for debugging
            try engineSupervisor.start(
                executable: executable,
                arguments: ["engine", "--socket", socketPath, "--insecure-dev"],
                environment: nil
            )

            // Wait for socket to appear with retries
            isEngineReady = await waitForSocket(path: socketPath)
        } catch {
            print("Failed to start engine: \(error)")
        }
    }

    private func waitForSocket(path: String) async -> Bool {
        for attempt in 1...maxAttempts {
            if FileManager.default.fileExists(atPath: path) {
                // Attempt to probe the socket
                return true
            }
            // Exponential backoff
            let delay = UInt64(pow(2.0, Double(attempt)) * Double(baseDelayNS))
            try? await Task.sleep(nanoseconds: delay)
        }
        return false
    }

    func stopEngine() {
        engineSupervisor.stop()
        isEngineReady = false
    }
}
