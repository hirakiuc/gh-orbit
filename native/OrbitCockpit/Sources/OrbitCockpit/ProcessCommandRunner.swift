import Foundation

enum CommandRunnerError: Error, Equatable {
    case failed(exitCode: Int32, standardError: String)
    case invalidOutputEncoding
}

private final class CapturedData: @unchecked Sendable {
    private let lock = NSLock()
    private var value = Data()

    func set(_ data: Data) {
        lock.lock()
        value = data
        lock.unlock()
    }

    func get() -> Data {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

struct ProcessCommandRunner: CommandRunning {
    func run(_ invocation: CommandInvocation) throws -> String {
        let process = Process()
        process.executableURL = invocation.executable
        process.arguments = invocation.arguments
        process.currentDirectoryURL = invocation.workingDirectory

        let output = Pipe()
        let error = Pipe()
        process.standardOutput = output
        process.standardError = error
        try process.run()

        let group = DispatchGroup()
        let outputData = CapturedData()
        let errorData = CapturedData()
        group.enter()
        DispatchQueue.global().async {
            outputData.set(output.fileHandleForReading.readDataToEndOfFile())
            group.leave()
        }
        group.enter()
        DispatchQueue.global().async {
            errorData.set(error.fileHandleForReading.readDataToEndOfFile())
            group.leave()
        }
        process.waitUntilExit()
        group.wait()

        guard let standardOutput = String(bytes: outputData.get(), encoding: .utf8) else {
            throw CommandRunnerError.invalidOutputEncoding
        }
        guard process.terminationStatus == 0 else {
            let standardError = String(bytes: errorData.get(), encoding: .utf8) ?? "Invalid UTF-8 output"
            throw CommandRunnerError.failed(exitCode: process.terminationStatus, standardError: standardError)
        }
        return standardOutput
    }
}
