import Foundation

struct TerminalLaunchRequest: Equatable {
    let executable: URL
    let arguments: [String]
    let environment: [String: String]?
    let currentDirectoryURL: URL?
}

@MainActor
protocol TerminalSessionCreating {
    func makeSession(
        request: TerminalLaunchRequest,
        onTerminate: @escaping (Int32?) -> Void
    ) -> TerminalProcessSession
}

protocol CodexExecutableResolving {
    func resolve() throws -> URL
}

enum CodexExecutableResolutionError: Error, Equatable, LocalizedError {
    case missingExecutable(String)

    var errorDescription: String? {
        switch self {
        case .missingExecutable(let message):
            message
        }
    }
}

struct CodexExecutableResolver: CodexExecutableResolving {
    private let fileManager: FileManager
    private let environment: [String: String]

    init(fileManager: FileManager = .default, environment: [String: String] = ProcessInfo.processInfo.environment) {
        self.fileManager = fileManager
        self.environment = environment
    }

    func resolve() throws -> URL {
        if let override = environment["CODEX_BIN"], !override.isEmpty {
            let url = URL(fileURLWithPath: override)
            if fileManager.isExecutableFile(atPath: url.path) {
                return url
            }
        }

        let paths = (environment["PATH"] ?? "")
            .split(separator: ":")
            .map(String.init)

        for path in paths {
            let url = URL(fileURLWithPath: path).appendingPathComponent("codex")
            if fileManager.isExecutableFile(atPath: url.path) {
                return url
            }
        }

        let fallbacks = [
            "/usr/local/bin/codex",
            "/opt/homebrew/bin/codex",
        ]

        for path in fallbacks {
            let url = URL(fileURLWithPath: path)
            if fileManager.isExecutableFile(atPath: url.path) {
                return url
            }
        }

        throw CodexExecutableResolutionError.missingExecutable(
            "Codex CLI not found. Install `codex` or set CODEX_BIN to the executable path.")
    }
}

protocol RepositoryInstructionReading {
    func readInstructions(from repositoryRoot: URL) -> String?
}

struct RepositoryInstructionFileReader: RepositoryInstructionReading {
    private let fileManager: FileManager

    init(fileManager: FileManager = .default) {
        self.fileManager = fileManager
    }

    func readInstructions(from repositoryRoot: URL) -> String? {
        let url = repositoryRoot.appendingPathComponent("AGENTS.md", isDirectory: false)
        guard fileManager.fileExists(atPath: url.path),
            let data = try? Data(contentsOf: url),
            let contents = String(data: data, encoding: .utf8)?
                .trimmingCharacters(in: .whitespacesAndNewlines),
            !contents.isEmpty
        else {
            return nil
        }
        return contents
    }
}

struct CodexReviewPromptContract: Equatable {
    static let version = 1

    let renderedPrompt: String
}

protocol CodexReviewPromptBuilding {
    func buildPrompt(for record: ReviewWorkspaceRecord) -> CodexReviewPromptContract
}

struct CodexReviewPromptBuilder: CodexReviewPromptBuilding {
    let instructionReader: RepositoryInstructionReading

    init(instructionReader: RepositoryInstructionReading = RepositoryInstructionFileReader()) {
        self.instructionReader = instructionReader
    }

    func buildPrompt(for record: ReviewWorkspaceRecord) -> CodexReviewPromptContract {
        let instructions =
            instructionReader.readInstructions(from: record.sourceClonePath)
            ?? "No repository-specific AGENTS.md instructions were found."

        let renderedPrompt = """
            [gh-orbit-review-contract/v\(CodexReviewPromptContract.version)]
            Objective: Review the prepared pull request worktree and print findings to this terminal only.

            Pull request:
            - Repository: \(record.repository.host)/\(record.repository.owner)/\(record.repository.name)
            - PR number: \(record.pullRequestNumber)
            - PR URL: \(record.pullRequestURL.absoluteString)
            - Head repository: \(record.headRepository.host)/\(record.headRepository.owner)/\(record.headRepository.name)
            - Head branch: \(record.headBranch)
            - Head SHA: \(record.headSHA)
            - Review worktree: \(record.worktreePath.path)

            Repository instructions (AGENTS.md):
            \(instructions)

            Review-only rules:
            - Stay within the prepared review worktree above.
            - Do not push, merge, comment on GitHub, or submit review results automatically.
            - Do not print or persist secrets, credentials, or tokens.
            - Print findings and final status directly to this terminal session only.

            Expected terminal output:
            - A concise review summary.
            - Findings with severity and file references when applicable.
            - A final terminal-only completion note indicating whether issues were found.
            """

        return CodexReviewPromptContract(renderedPrompt: renderedPrompt)
    }
}

@MainActor
protocol ReviewWorkspaceCodexLaunching {
    func launchSession(
        for record: ReviewWorkspaceRecord,
        environment: [String: String],
        onTerminate: @escaping (Int32?) -> Void
    ) throws -> TerminalProcessSession
}

@MainActor
struct CodexReviewWorkspaceLauncher: ReviewWorkspaceCodexLaunching {
    let executableResolver: any CodexExecutableResolving
    let promptBuilder: any CodexReviewPromptBuilding
    let terminalSessionFactory: any TerminalSessionCreating

    init(
        executableResolver: any CodexExecutableResolving = CodexExecutableResolver(),
        promptBuilder: any CodexReviewPromptBuilding = CodexReviewPromptBuilder(),
        terminalSessionFactory: any TerminalSessionCreating
    ) {
        self.executableResolver = executableResolver
        self.promptBuilder = promptBuilder
        self.terminalSessionFactory = terminalSessionFactory
    }

    func launchSession(
        for record: ReviewWorkspaceRecord,
        environment: [String: String],
        onTerminate: @escaping (Int32?) -> Void
    ) throws -> TerminalProcessSession {
        let executable = try executableResolver.resolve()
        let prompt = promptBuilder.buildPrompt(for: record)
        let request = TerminalLaunchRequest(
            executable: executable,
            arguments: [],
            environment: environment,
            currentDirectoryURL: record.worktreePath
        )
        let session = terminalSessionFactory.makeSession(request: request, onTerminate: onTerminate)
        session.send(string: prompt.renderedPrompt + "\n")
        return session
    }
}
