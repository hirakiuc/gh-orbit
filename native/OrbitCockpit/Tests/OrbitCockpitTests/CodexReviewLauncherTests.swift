import AppKit
import Foundation
import Testing

@testable import OrbitCockpit

private final class SpyInstructionReader: RepositoryInstructionReading {
    let contents: String?
    private(set) var readRoots: [URL] = []

    init(contents: String?) {
        self.contents = contents
    }

    func readInstructions(from repositoryRoot: URL) -> String? {
        readRoots.append(repositoryRoot)
        return contents
    }
}

private struct StubCodexExecutableResolver: CodexExecutableResolving {
    let result: Result<URL, Error>

    func resolve() throws -> URL {
        try result.get()
    }
}

@MainActor
private final class MockTerminalSessionFactory: TerminalSessionCreating {
    private(set) var requests: [TerminalLaunchRequest] = []
    private let session: MockTerminalSession

    init(session: MockTerminalSession = MockTerminalSession()) {
        self.session = session
    }

    func makeSession(
        request: TerminalLaunchRequest,
        onTerminate: @escaping (Int32?) -> Void
    ) -> TerminalProcessSession {
        requests.append(request)
        return session
    }
}

@Suite("Codex review launcher")
struct CodexReviewLauncherTests {
    private func sampleRecord() throws -> ReviewWorkspaceRecord {
        try .init(
            id: #require(UUID(uuidString: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE")),
            repository: .init(host: "github.com", owner: "acme", name: "orbit"),
            pullRequestNumber: 42,
            pullRequestURL: #require(URL(string: "https://github.com/acme/orbit/pull/42")),
            sourceClonePath: URL(fileURLWithPath: "/tmp/source", isDirectory: true),
            sourceCloneRemoteURL: "https://token@example.invalid/acme/orbit.git",
            headRepository: .init(host: "github.com", owner: "contrib", name: "orbit"),
            headCloneURL: #require(URL(string: "https://token@example.invalid/contrib/orbit.git")),
            headBranch: "feature/review",
            headSHA: "0123456789abcdef0123456789abcdef01234567",
            worktreePath: URL(fileURLWithPath: "/tmp/worktrees/pr-42", isDirectory: true),
            createdAt: .distantPast,
            updatedAt: .distantPast,
            state: .active
        )
    }

    @Test
    func promptIncludesRequiredContextAndExcludesCloneSecrets() throws {
        let record = try sampleRecord()
        let instructionReader = SpyInstructionReader(contents: "Follow AGENTS instructions.")
        let prompt = CodexReviewPromptBuilder(
            instructionReader: instructionReader
        ).buildPrompt(for: record)

        #expect(prompt.renderedPrompt.contains("[gh-orbit-review-contract/v1]"))
        #expect(prompt.renderedPrompt.contains("PR number: 42"))
        #expect(prompt.renderedPrompt.contains(record.pullRequestURL.absoluteString))
        #expect(prompt.renderedPrompt.contains(record.headSHA))
        #expect(prompt.renderedPrompt.contains(record.worktreePath.path))
        #expect(prompt.renderedPrompt.contains("Follow AGENTS instructions."))
        #expect(!prompt.renderedPrompt.contains(record.sourceCloneRemoteURL))
        #expect(!prompt.renderedPrompt.contains(record.headCloneURL.absoluteString))
        #expect(instructionReader.readRoots == [record.worktreePath])
        #expect(instructionReader.readRoots != [record.sourceClonePath])
    }

    @Test @MainActor
    func launcherUsesStructuredLaunchRequestAndTargetsPreparedWorktree() throws {
        let record = try sampleRecord()
        let session = MockTerminalSession()
        let factory = MockTerminalSessionFactory(session: session)
        let launcher = CodexReviewWorkspaceLauncher(
            executableResolver: StubCodexExecutableResolver(
                result: .success(URL(fileURLWithPath: "/usr/local/bin/codex"))),
            promptBuilder: CodexReviewPromptBuilder(
                instructionReader: SpyInstructionReader(contents: "Follow AGENTS instructions.")),
            terminalSessionFactory: factory
        )

        _ = try launcher.launchSession(
            for: record,
            environment: ["GH_ORBIT_REQUIRE_ENGINE": "1"],
            onTerminate: { _ in }
        )

        let request = try #require(factory.requests.first)
        #expect(request.executable.path == "/usr/local/bin/codex")
        #expect(request.arguments.isEmpty)
        #expect(request.environment == ["GH_ORBIT_REQUIRE_ENGINE": "1"])
        #expect(request.currentDirectoryURL == record.worktreePath)
        #expect(session.sendCalls.count == 1)
        #expect(session.sendCalls[0].contains(record.headSHA))
        #expect(session.sendCalls[0].hasSuffix("\n"))
    }

    @Test @MainActor
    func swiftTermAdapterForwardsCurrentDirectoryAsFirstClassField() throws {
        var launchedExecutable: String?
        var launchedArgs: [String] = []
        var launchedEnvironment: [String]?
        var launchedCurrentDirectory: String?
        let adapter = SwiftTermAdapter(
            processStarter: { _, request in
                launchedExecutable = request.executable.path
                launchedArgs = request.arguments
                launchedEnvironment = request.environment.map { $0.map { "\($0.key)=\($0.value)" } }
                launchedCurrentDirectory = request.currentDirectoryURL?.path
            })
        let request = TerminalLaunchRequest(
            executable: URL(fileURLWithPath: "/usr/local/bin/codex"),
            arguments: ["exec"],
            environment: ["FOO": "bar"],
            currentDirectoryURL: URL(fileURLWithPath: "/tmp/worktree", isDirectory: true)
        )

        adapter.startProcess(request: request)

        #expect(launchedExecutable == "/usr/local/bin/codex")
        #expect(launchedArgs == ["exec"])
        #expect(launchedEnvironment == ["FOO=bar"])
        #expect(launchedCurrentDirectory == "/tmp/worktree")
    }
}
