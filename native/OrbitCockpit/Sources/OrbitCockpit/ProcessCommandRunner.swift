import Foundation

enum CommandRunnerError: Error, Equatable {
    case failed(exitCode: Int32, standardError: String)
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
        process.waitUntilExit()

        let standardOutput = String(decoding: output.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
        guard process.terminationStatus == 0 else {
            let standardError = String(decoding: error.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
            throw CommandRunnerError.failed(exitCode: process.terminationStatus, standardError: standardError)
        }
        return standardOutput
    }
}
