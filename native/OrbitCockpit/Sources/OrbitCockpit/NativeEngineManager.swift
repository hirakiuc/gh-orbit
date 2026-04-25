import Combine
import Foundation

/// NativeEngineManager manages the persistent gh-orbit engine process.
@MainActor
class NativeEngineManager: ObservableObject {
    @Published var isEngineReady: Bool = false

    private var engineSupervisor = ProcessSupervisor()
    private var socketPath: String

    private let maxAttempts: Int
    private let baseDelayNS: UInt64

    // App Group for shared communication within Sandbox
    private let appGroupID = "com.hirakiuc.gh-orbit.cockpit"

    init(
        socketPath: String? = nil,
        maxAttempts: Int = 10,
        baseDelayNS: UInt64 = 50_000_000,
        onLog: ((String, LogLevel) -> Void)? = nil
    ) {
        self.maxAttempts = maxAttempts
        self.baseDelayNS = baseDelayNS

        if let socketPath = socketPath {
            self.socketPath = socketPath
            onLog?("Using explicit socket path: \(self.socketPath)", .debug)
        } else {
            // Resolve socket path: prioritize shared App Group container for Sandbox compliance
            if let groupURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: appGroupID) {
                self.socketPath = groupURL.appendingPathComponent("engine.sock").path
                onLog?("Resolved App Group socket path: \(self.socketPath)", .debug)
            } else {
                // Fallback for non-sandboxed dev mode
                let runtimeDir =
                    ProcessInfo.processInfo.environment["XDG_RUNTIME_DIR"]
                    ?? (FileManager.default.homeDirectoryForCurrentUser.path + "/.local/run/gh-orbit")
                self.socketPath = runtimeDir + "/engine.sock"
                onLog?("Resolved Fallback socket path: \(self.socketPath)", .debug)
            }
        }

        // Set the supervisor's logging closure
        engineSupervisor.onLog = { message, level in
            onLog?(message, level)
        }
    }

    func startEngine(executable: URL) async {
        guard !engineSupervisor.isRunning else { return }

        do {
            engineSupervisor.onLog?("Starting gh-orbit engine with executable: \(executable.path)", .debug)
            // Start engine with verbose logging for debugging
            try engineSupervisor.start(
                executable: executable,
                arguments: ["engine", "--socket", socketPath, "--insecure-dev"],
                environment: nil
            )

            // Wait for socket to appear with retries
            isEngineReady = await waitForSocket(path: socketPath)
            if isEngineReady {
                engineSupervisor.onLog?("Engine is ready (socket found).", .info)
            } else {
                engineSupervisor.onLog?("Engine failed to become ready (socket not found).", .error)
            }
        } catch {
            engineSupervisor.onLog?("Failed to start engine: \(error)", .error)
            print("Failed to start engine: \(error)")
        }
    }

    private func waitForSocket(path: String) async -> Bool {
        for attempt in 1...maxAttempts {
            if FileManager.default.fileExists(atPath: path) {
                return true
            }
            // Exponential backoff
            let delay = UInt64(pow(2.0, Double(attempt)) * Double(baseDelayNS))
            try? await Task.sleep(nanoseconds: delay)
        }
        return false
    }

    func stopEngine() {
        engineSupervisor.onLog?("Stopping gh-orbit engine.", .info)
        engineSupervisor.stop()
        isEngineReady = false
    }
}
