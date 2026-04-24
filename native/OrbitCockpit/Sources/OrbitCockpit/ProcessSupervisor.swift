import Combine
import Foundation

/// ProcessSupervisor handles the lifecycle and observability of a subprocess.
@MainActor
class ProcessSupervisor: ObservableObject {
    @Published var isRunning: Bool = false
    @Published var exitCode: Int32?
    @Published var lastError: String?

    private var process: Process?
    private let outputPipe = Pipe()
    private let errorPipe = Pipe()

    /// Bounded buffer for logs (approx 5MB)
    private var logBuffer: [String] = []
    private let maxLogLines = 5000

    func start(executable: URL, arguments: [String], environment: [String: String]?) throws {
        let process = Process()
        process.executableURL = executable
        process.arguments = arguments

        // Harden environment
        var env = ProcessInfo.processInfo.environment
        if let customEnv = environment {
            env.merge(customEnv) { (_, new) in new }
        }

        // Ensure critical paths are present
        if env["PATH"] == nil {
            env["PATH"] = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
        }

        process.environment = env
        process.standardOutput = outputPipe
        process.standardError = errorPipe

        errorPipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            if let line = String(data: data, encoding: .utf8) {
                DispatchQueue.main.async {
                    self?.appendLog(line)
                    self?.lastError = line
                }
            }
        }

        process.terminationHandler = { [weak self] process in
            DispatchQueue.main.async {
                self?.isRunning = false
                self?.exitCode = process.terminationStatus
                self?.errorPipe.fileHandleForReading.readabilityHandler = nil
                self?.outputPipe.fileHandleForReading.readabilityHandler = nil
            }
        }

        try process.run()
        self.process = process
        self.isRunning = true
    }

    func stop() {
        process?.terminate()
    }

    private func appendLog(_ line: String) {
        logBuffer.append(line)
        if logBuffer.count > maxLogLines {
            logBuffer.removeFirst()
        }
    }

    func getLogs() -> String {
        return logBuffer.joined()
    }
}
