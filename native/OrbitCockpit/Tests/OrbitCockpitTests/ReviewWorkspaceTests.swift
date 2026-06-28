import Foundation
import Testing

@testable import OrbitCockpit

private final class MockReviewWorkspaceLifecycleController: ReviewWorkspaceLifecycleControlling {
    var restoredResult: ReviewWorkspaceReconciliationResult = .init(records: [], orphanedWorktrees: [])
    var createdRecord: ReviewWorkspaceRecord?
    var cleanupResult: ReviewWorkspaceCleanupResult = .removed
    var restoreCallCount = 0
    var cleanupCalls: [ReviewWorkspaceRecord] = []

    func createWorkspace(
        for pullRequest: ResolvedPullRequest,
        workspaceID: UUID,
        now: Date
    ) throws -> ReviewWorkspaceRecord {
        if let createdRecord {
            return createdRecord
        }
        return .init(
            id: workspaceID,
            repository: pullRequest.base,
            pullRequestNumber: pullRequest.number,
            pullRequestURL: pullRequest.url,
            sourceClonePath: pullRequest.localClonePath,
            sourceCloneRemoteURL: pullRequest.localCloneRemoteURL,
            headRepository: pullRequest.head,
            headCloneURL: pullRequest.headCloneURL,
            headBranch: pullRequest.headBranch,
            headSHA: pullRequest.headSHA,
            worktreePath: URL(fileURLWithPath: "/tmp/\(workspaceID.uuidString)", isDirectory: true),
            createdAt: now,
            updatedAt: now,
            state: .active)
    }

    func restoreWorkspaces(now: Date) throws -> ReviewWorkspaceReconciliationResult {
        restoreCallCount += 1
        return restoredResult
    }

    func cleanupWorkspace(_ record: ReviewWorkspaceRecord, now: Date) throws -> ReviewWorkspaceCleanupResult {
        cleanupCalls.append(record)
        return cleanupResult
    }
}

private final class MockPullRequestResolver: PullRequestResolving, @unchecked Sendable {
    private let lock = NSLock()
    var resolveCalls: [(RepositoryIdentity, Int)] = []
    var result: Result<ResolvedPullRequest, Error>
    var waitForResume = false
    private let semaphore = DispatchSemaphore(value: 0)

    init(result: Result<ResolvedPullRequest, Error>) {
        self.result = result
    }

    func resolve(repository: RepositoryIdentity, number: Int) throws -> ResolvedPullRequest {
        lock.lock()
        resolveCalls.append((repository, number))
        let shouldWait = waitForResume
        lock.unlock()

        if shouldWait {
            semaphore.wait()
        }
        return try result.get()
    }

    func resume() {
        semaphore.signal()
    }
}

@MainActor
private final class MockReviewWorkspaceCodexLauncher: ReviewWorkspaceCodexLaunching {
    private(set) var launchedRecords: [ReviewWorkspaceRecord] = []
    private let session: MockTerminalSession

    init(session: MockTerminalSession = MockTerminalSession()) {
        self.session = session
    }

    func launchSession(
        for record: ReviewWorkspaceRecord,
        environment: [String: String],
        onTerminate: @escaping (Int32?) -> Void
    ) throws -> TerminalProcessSession {
        launchedRecords.append(record)
        return session
    }
}

@Suite("Review workspace lifecycle")
struct ReviewWorkspaceTests {
    private func sampleRequest() -> NativeReviewRequest {
        NativeReviewRequest(
            repository: .init(host: "github.com", owner: "acme", name: "orbit"),
            pullRequestNumber: 42,
            title: "Needs review",
            subtitle: "Review request fixture"
        )
    }

    private func sampleResolvedPullRequest(
        headSHA: String = "0123456789abcdef0123456789abcdef01234567"
    ) -> ResolvedPullRequest {
        .init(
            base: .init(host: "github.com", owner: "acme", name: "orbit"),
            localClonePath: URL(fileURLWithPath: "/tmp/source", isDirectory: true),
            localCloneRemoteURL: "git@github.com:acme/orbit.git",
            number: 42,
            url: URL(string: "https://github.com/acme/orbit/pull/42")!,
            head: .init(host: "github.com", owner: "contrib", name: "orbit"),
            headCloneURL: URL(fileURLWithPath: "/tmp/origin.git", isDirectory: true),
            headBranch: "feature/review",
            headSHA: headSHA
        )
    }

    @Test @MainActor
    func workspacesUseIndependentReservedPanes() throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let manager = ReviewWorkspaceManager(terminalManager: terminalManager)
        let first = try #require(manager.createFixtureWorkspace(named: "same"))
        let second = try #require(manager.createFixtureWorkspace(named: "same"))
        #expect(first.paneName != second.paneName)
        #expect(first.paneName.hasPrefix(TerminalManager.workspacePanePrefix))
        #expect(terminalManager.state(for: first.paneName) == .launching)
        terminalManager.installSession(MockTerminalSession(), for: first.paneName)
        #expect(terminalManager.engine(for: first.paneName) == nil)
    }

    @Test @MainActor
    func exitRetainsEngineUntilDismissed() throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let manager = ReviewWorkspaceManager(terminalManager: terminalManager)
        let workspace = try #require(manager.createFixtureWorkspace(named: "review"))
        let session = MockTerminalSession()
        manager.install(session, for: workspace.id)
        manager.requestTermination(for: workspace.id)
        #expect(session.terminateCalls == 1)
        manager.reportTerminalExit(for: workspace.id, exitCode: 0)
        #expect(terminalManager.engine(for: workspace.paneName) === session.engine)
        manager.dismiss(workspace.id)
        #expect(terminalManager.engine(for: workspace.paneName) == nil)
    }

    @Test @MainActor
    func callbacksRespectLifecycleAndDismissalIsIsolated() throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let manager = ReviewWorkspaceManager(terminalManager: terminalManager)
        let first = try #require(manager.createFixtureWorkspace(named: "first"))
        let second = try #require(manager.createFixtureWorkspace(named: "second"))
        let firstSession = MockTerminalSession()
        manager.install(firstSession, for: first.id)
        manager.reportSetupFailure(for: second.id, message: "setup failed")
        manager.reportTerminalExit(for: second.id, exitCode: 0)
        #expect(manager.workspace(forPaneName: second.paneName)?.state == .failed("setup failed"))
        manager.requestTermination(for: first.id)
        #expect(manager.workspace(forPaneName: first.paneName)?.state == .terminating)
        manager.reportSetupFailure(for: first.id, message: "late failure")
        #expect(manager.workspace(forPaneName: first.paneName)?.state == .terminating)
        manager.reportTerminalExit(for: first.id, exitCode: 0)
        manager.dismiss(first.id)
        #expect(manager.workspace(forPaneName: first.paneName) == nil)
        #expect(manager.workspace(forPaneName: second.paneName)?.state == .failed("setup failed"))
    }

    @Test @MainActor
    func restoresPersistedManagedWorkspacesAndOrphansOnStartup() throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let lifecycle = MockReviewWorkspaceLifecycleController()
        let workspaceID = try #require(UUID(uuidString: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"))
        let pullRequestURL = try #require(URL(string: "https://github.com/acme/orbit/pull/12"))
        let record = ReviewWorkspaceRecord(
            id: workspaceID,
            repository: .init(host: "github.com", owner: "acme", name: "orbit"),
            pullRequestNumber: 12,
            pullRequestURL: pullRequestURL,
            sourceClonePath: URL(fileURLWithPath: "/tmp/source", isDirectory: true),
            sourceCloneRemoteURL: "git@github.com:acme/orbit.git",
            headRepository: .init(host: "github.com", owner: "acme", name: "orbit"),
            headCloneURL: URL(fileURLWithPath: "/tmp/origin.git", isDirectory: true),
            headBranch: "main",
            headSHA: "0123456789abcdef0123456789abcdef01234567",
            worktreePath: URL(fileURLWithPath: "/tmp/worktree", isDirectory: true),
            createdAt: .distantPast,
            updatedAt: .distantPast,
            state: .active)
        lifecycle.restoredResult = .init(
            records: [record],
            orphanedWorktrees: [
                .init(
                    sourceClonePath: URL(fileURLWithPath: "/tmp/source", isDirectory: true),
                    worktreePath: URL(fileURLWithPath: "/tmp/orphan", isDirectory: true))
            ])

        let manager = ReviewWorkspaceManager(terminalManager: terminalManager, lifecycleController: lifecycle)
        manager.restoreManagedWorkspacesIfNeeded()

        #expect(lifecycle.restoreCallCount == 1)
        #expect(manager.workspaces.count == 2)
        #expect(manager.workspaces[0].state == .available)
        #expect(manager.workspaces[0].record == record)
        #expect(manager.workspaces[1].state == .cleanupRequired("Orphaned managed worktree at /tmp/orphan."))
        manager.restoreManagedWorkspacesIfNeeded()
        #expect(lifecycle.restoreCallCount == 1)
    }

    @Test @MainActor
    func terminalExitTriggersManagedCleanupHandling() throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let lifecycle = MockReviewWorkspaceLifecycleController()
        let manager = ReviewWorkspaceManager(terminalManager: terminalManager, lifecycleController: lifecycle)
        let workspaceID = try #require(UUID(uuidString: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"))
        let pullRequestURL = try #require(URL(string: "https://github.com/acme/orbit/pull/12"))
        let record = ReviewWorkspaceRecord(
            id: workspaceID,
            repository: .init(host: "github.com", owner: "acme", name: "orbit"),
            pullRequestNumber: 12,
            pullRequestURL: pullRequestURL,
            sourceClonePath: URL(fileURLWithPath: "/tmp/source", isDirectory: true),
            sourceCloneRemoteURL: "git@github.com:acme/orbit.git",
            headRepository: .init(host: "github.com", owner: "acme", name: "orbit"),
            headCloneURL: URL(fileURLWithPath: "/tmp/origin.git", isDirectory: true),
            headBranch: "main",
            headSHA: "0123456789abcdef0123456789abcdef01234567",
            worktreePath: URL(fileURLWithPath: "/tmp/worktree", isDirectory: true),
            createdAt: .distantPast,
            updatedAt: .distantPast,
            state: .active)
        let workspace = try #require(
            manager.attachManagedWorkspace(record, displayName: "PR #12", initialState: .preparing))
        let session = MockTerminalSession()

        manager.install(session, for: workspace.id)
        let updatedRecord = ReviewWorkspaceRecord(
            id: record.id,
            repository: record.repository,
            pullRequestNumber: record.pullRequestNumber,
            pullRequestURL: record.pullRequestURL,
            sourceClonePath: record.sourceClonePath,
            sourceCloneRemoteURL: record.sourceCloneRemoteURL,
            headRepository: record.headRepository,
            headCloneURL: record.headCloneURL,
            headBranch: record.headBranch,
            headSHA: record.headSHA,
            worktreePath: record.worktreePath,
            createdAt: record.createdAt,
            updatedAt: .now,
            state: .cleanupRequired)
        lifecycle.cleanupResult = .cleanupRequired(updatedRecord)

        manager.requestTermination(for: workspace.id)
        manager.reportTerminalExit(for: workspace.id, exitCode: 0)

        #expect(lifecycle.cleanupCalls == [record])
        #expect(
            manager.workspace(forPaneName: workspace.paneName)?.state
                == .cleanupRequired("Cleanup required for /tmp/worktree."))
    }

    @Test @MainActor
    func duplicateStartsReusePreparingPlaceholderBeforeResolutionCompletes() async throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let lifecycle = MockReviewWorkspaceLifecycleController()
        let resolver = MockPullRequestResolver(result: .success(sampleResolvedPullRequest()))
        resolver.waitForResume = true
        let codexLauncher = MockReviewWorkspaceCodexLauncher()
        let manager = ReviewWorkspaceManager(
            terminalManager: terminalManager,
            lifecycleController: lifecycle,
            codexLauncher: codexLauncher,
            pullRequestResolverFactory: { resolver }
        )
        var focusedPanes: [String] = []
        manager.onPaneFocusRequested = { focusedPanes.append($0) }

        let firstID = try #require(manager.startReviewWorkspace(for: sampleRequest()))
        try await Task.sleep(nanoseconds: 50_000_000)
        let secondID = try #require(manager.startReviewWorkspace(for: sampleRequest()))

        #expect(firstID == secondID)
        #expect(manager.workspaces.count == 1)
        #expect(manager.workspaces[0].state == .preparing)
        #expect(resolver.resolveCalls.count == 1)
        #expect(focusedPanes.count == 2)
        #expect(focusedPanes[0] == focusedPanes[1])

        resolver.resume()
        try await Task.sleep(nanoseconds: 50_000_000)
        #expect(manager.workspaces[0].state == .running)
    }

    @Test @MainActor
    func identicalResolvedWorkspaceIsFocusedInsteadOfDuplicated() async throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let lifecycle = MockReviewWorkspaceLifecycleController()
        let resolver = MockPullRequestResolver(result: .success(sampleResolvedPullRequest()))
        let codexLauncher = MockReviewWorkspaceCodexLauncher()
        let manager = ReviewWorkspaceManager(
            terminalManager: terminalManager,
            lifecycleController: lifecycle,
            codexLauncher: codexLauncher,
            pullRequestResolverFactory: { resolver }
        )
        var focusedPanes: [String] = []
        manager.onPaneFocusRequested = { focusedPanes.append($0) }

        _ = manager.startReviewWorkspace(for: sampleRequest())
        try await Task.sleep(nanoseconds: 100_000_000)
        _ = manager.startReviewWorkspace(for: sampleRequest())
        try await Task.sleep(nanoseconds: 100_000_000)

        #expect(manager.workspaces.count == 1)
        #expect(manager.workspaces[0].state == .running)
        #expect(codexLauncher.launchedRecords.count == 1)
        #expect(resolver.resolveCalls.count == 2)
        #expect(focusedPanes.last == manager.workspaces[0].paneName)
    }

    @Test @MainActor
    func changedHeadAllowsSecondWorkspaceLaunch() async throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let lifecycle = MockReviewWorkspaceLifecycleController()
        let resolver = MockPullRequestResolver(result: .success(sampleResolvedPullRequest()))
        let codexLauncher = MockReviewWorkspaceCodexLauncher()
        let manager = ReviewWorkspaceManager(
            terminalManager: terminalManager,
            lifecycleController: lifecycle,
            codexLauncher: codexLauncher,
            pullRequestResolverFactory: { resolver }
        )

        _ = manager.startReviewWorkspace(for: sampleRequest())
        try await Task.sleep(nanoseconds: 100_000_000)

        resolver.result = .success(sampleResolvedPullRequest(headSHA: "fedcba9876543210fedcba9876543210fedcba98"))
        _ = manager.startReviewWorkspace(for: sampleRequest())
        try await Task.sleep(nanoseconds: 100_000_000)

        #expect(manager.workspaces.count == 2)
        #expect(manager.workspaces.allSatisfy { $0.state == .running })
    }

    @Test @MainActor
    func resolutionFailuresRemainVisibleAsFailedWorkspace() async throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let lifecycle = MockReviewWorkspaceLifecycleController()
        let resolver = MockPullRequestResolver(result: .failure(PullRequestResolutionError.noLocalClone))
        let manager = ReviewWorkspaceManager(
            terminalManager: terminalManager,
            lifecycleController: lifecycle,
            pullRequestResolverFactory: { resolver }
        )

        let workspaceID = try #require(manager.startReviewWorkspace(for: sampleRequest()))
        try await Task.sleep(nanoseconds: 100_000_000)

        let workspace = try #require(manager.workspaces.first(where: { $0.id == workspaceID }))
        #expect(
            workspace.state
                == .failed(
                    "No local clone matched the selected repository. Ensure the repository is available through `ghq`."
                ))
        #expect(workspace.diagnostics.map(\.category) == [.launch, .resolution, .resolution])
        #expect(workspace.diagnostics.last?.level == .error)
    }

    @Test @MainActor
    func duplicateReuseAppendsDiagnosticOnlyToReusedWorkspace() async throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let lifecycle = MockReviewWorkspaceLifecycleController()
        let resolver = MockPullRequestResolver(result: .success(sampleResolvedPullRequest()))
        resolver.waitForResume = true
        let manager = ReviewWorkspaceManager(
            terminalManager: terminalManager,
            lifecycleController: lifecycle,
            pullRequestResolverFactory: { resolver }
        )

        let workspaceID = try #require(manager.startReviewWorkspace(for: sampleRequest()))
        try await Task.sleep(nanoseconds: 50_000_000)
        _ = manager.startReviewWorkspace(for: sampleRequest())

        let workspace = try #require(manager.workspaces.first(where: { $0.id == workspaceID }))
        #expect(workspace.diagnostics.count == 3)
        #expect(
            workspace.diagnostics.last?.message
                == "Reused the existing in-progress workspace for PR #42."
        )
    }

    @Test @MainActor
    func diagnosticsRemainScopedToEachWorkspace() async throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let lifecycle = MockReviewWorkspaceLifecycleController()
        let resolver = MockPullRequestResolver(result: .success(sampleResolvedPullRequest()))
        let codexLauncher = MockReviewWorkspaceCodexLauncher()
        let manager = ReviewWorkspaceManager(
            terminalManager: terminalManager,
            lifecycleController: lifecycle,
            codexLauncher: codexLauncher,
            pullRequestResolverFactory: { resolver }
        )

        let firstID = try #require(manager.startReviewWorkspace(for: sampleRequest()))
        try await Task.sleep(nanoseconds: 100_000_000)

        resolver.result = .success(sampleResolvedPullRequest(headSHA: "fedcba9876543210fedcba9876543210fedcba98"))
        let secondID = try #require(manager.startReviewWorkspace(for: sampleRequest()))
        try await Task.sleep(nanoseconds: 100_000_000)

        let first = try #require(manager.workspaces.first(where: { $0.id == firstID }))
        let second = try #require(manager.workspaces.first(where: { $0.id == secondID }))

        #expect(first.diagnostics.contains { $0.message.contains("0123456789abcdef0123456789abcdef01234567") })
        #expect(!first.diagnostics.contains { $0.message.contains("fedcba9876543210fedcba9876543210fedcba98") })
        #expect(second.diagnostics.contains { $0.message.contains("fedcba9876543210fedcba9876543210fedcba98") })
        #expect(!second.diagnostics.contains { $0.message.contains("0123456789abcdef0123456789abcdef01234567") })
    }

    @Test @MainActor
    func restoredManagedWorkspacesAndOrphansRecordRestoreDiagnostics() throws {
        let terminalManager = TerminalManager(monitor: ActivityMonitor())
        let lifecycle = MockReviewWorkspaceLifecycleController()
        let workspaceID = try #require(UUID(uuidString: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"))
        let pullRequestURL = try #require(URL(string: "https://github.com/acme/orbit/pull/12"))
        let record = ReviewWorkspaceRecord(
            id: workspaceID,
            repository: .init(host: "github.com", owner: "acme", name: "orbit"),
            pullRequestNumber: 12,
            pullRequestURL: pullRequestURL,
            sourceClonePath: URL(fileURLWithPath: "/tmp/source", isDirectory: true),
            sourceCloneRemoteURL: "git@github.com:acme/orbit.git",
            headRepository: .init(host: "github.com", owner: "acme", name: "orbit"),
            headCloneURL: URL(fileURLWithPath: "/tmp/origin.git", isDirectory: true),
            headBranch: "main",
            headSHA: "0123456789abcdef0123456789abcdef01234567",
            worktreePath: URL(fileURLWithPath: "/tmp/worktree", isDirectory: true),
            createdAt: .distantPast,
            updatedAt: .distantPast,
            state: .active)
        lifecycle.restoredResult = .init(
            records: [record],
            orphanedWorktrees: [
                .init(
                    sourceClonePath: URL(fileURLWithPath: "/tmp/source", isDirectory: true),
                    worktreePath: URL(fileURLWithPath: "/tmp/orphan", isDirectory: true))
            ])

        let manager = ReviewWorkspaceManager(terminalManager: terminalManager, lifecycleController: lifecycle)
        manager.restoreManagedWorkspacesIfNeeded()

        let restored = try #require(manager.workspaces.first(where: { $0.record?.id == workspaceID }))
        let orphan = try #require(manager.workspaces.first(where: { $0.record == nil }))

        #expect(restored.diagnostics.map(\.category) == [.restore])
        #expect(restored.diagnostics.first?.level == .info)
        #expect(orphan.diagnostics.map(\.category) == [.restore])
        #expect(orphan.diagnostics.first?.level == .warning)
    }
}
