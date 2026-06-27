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

@Suite("Review workspace lifecycle")
struct ReviewWorkspaceTests {
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
}
