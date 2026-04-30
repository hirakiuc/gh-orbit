import Foundation

@MainActor
protocol EngineProcessSupervising: AnyObject {
    var isRunning: Bool { get }
    var onLog: ((String, LogLevel) -> Void)? { get set }
    func start(executable: URL, arguments: [String], environment: [String: String]?) throws
    func stop()
}

/// ProcessSupervisor handles the lifecycle and observability of a subprocess.
@MainActor
class ProcessSupervisor: ObservableObject, EngineProcessSupervising {
    @Published var isRunning: Bool = false
    @Published var exitCode: Int32?
    @Published var lastError: String?

    private var process: Process?
    private let outputPipe = Pipe()
    private let errorPipe = Pipe()

    /// Closure to push logs to a central monitor.
    var onLog: ((String, LogLevel) -> Void)?

    nonisolated func parseLogLevel(from line: String, defaultLevel: LogLevel) -> LogLevel {
        if line.contains("\"level\":\"DEBUG\"") || line.contains("\"level\":\"debug\"") {
            return .debug
        } else if line.contains("\"level\":\"INFO\"") || line.contains("\"level\":\"info\"") {
            return .info
        } else if line.contains("\"level\":\"WARN\"") || line.contains("\"level\":\"warn\"") {
            return .warning
        } else if line.contains("\"level\":\"ERROR\"") || line.contains("\"level\":\"error\"") {
            return .error
        }
        return defaultLevel
    }

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
                let level = self?.parseLogLevel(from: line, defaultLevel: .error) ?? .error
                DispatchQueue.main.async {
                    self?.onLog?(line, level)
                    self?.lastError = line
                }
            }
        }

        // Also capture stdout for debugging if needed
        outputPipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            guard !data.isEmpty else { return }
            if let line = String(data: data, encoding: .utf8) {
                let level = self?.parseLogLevel(from: line, defaultLevel: .info) ?? .info
                DispatchQueue.main.async {
                    self?.onLog?(line, level)
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
}
