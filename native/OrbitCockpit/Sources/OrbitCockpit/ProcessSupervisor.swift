import Combine
import Foundation

/// ProcessSupervisor handles the lifecycle and observability of a subprocess.
@MainActor
class ProcessSupervisor: ObservableObject {
    @Published var isRunning: Bool = false
    @Published var exitCode: Int32?
    @Published var lastError: String?

    /// The aggregated log stream for UI display, updated via debounced mechanism.
    @Published var fullLog: String = ""

    private var process: Process?
    private let outputPipe = Pipe()
    private let errorPipe = Pipe()

    /// Bounded buffer for logs (approx 5MB)
    private var logBuffer: [String] = []
    private let maxLogLines = 5000

    // Performance: Debounce UI updates during high-volume logs
    private var pendingLogs: Bool = false
    private var logTimer: Timer?

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
            guard !data.isEmpty else { return }
            if let line = String(data: data, encoding: .utf8) {
                DispatchQueue.main.async {
                    self?.appendLog(line)
                    self?.lastError = line
                }
            }
        }

        // Also capture stdout for debugging if needed
        outputPipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            guard !data.isEmpty else { return }
            if let line = String(data: data, encoding: .utf8) {
                DispatchQueue.main.async {
                    self?.appendLog(line)
                }
            }
        }

        process.terminationHandler = { [weak self] process in
            DispatchQueue.main.async {
                self?.isRunning = false
                self?.exitCode = process.terminationStatus
                self?.errorPipe.fileHandleForReading.readabilityHandler = nil
                self?.outputPipe.fileHandleForReading.readabilityHandler = nil
                self?.publishLogs()  // Final sync
            }
        }

        try process.run()
        self.process = process
        self.isRunning = true

        // Start debouncer
        startLogTimer()
    }

    func stop() {
        process?.terminate()
        logTimer?.invalidate()
        logTimer = nil
    }

    private func appendLog(_ line: String) {
        let isFirstLog = logBuffer.isEmpty
        logBuffer.append(line)
        if logBuffer.count > maxLogLines {
            logBuffer.removeFirst()
        }

        if isFirstLog {
            publishLogs()
        } else {
            pendingLogs = true
        }
    }

    private func startLogTimer() {
        logTimer = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                if self?.pendingLogs == true {
                    self?.publishLogs()
                }
            }
        }
    }

    private func publishLogs() {
        fullLog = logBuffer.joined()
        pendingLogs = false
    }

    func getLogs() -> String {
        return logBuffer.joined()
    }
}
