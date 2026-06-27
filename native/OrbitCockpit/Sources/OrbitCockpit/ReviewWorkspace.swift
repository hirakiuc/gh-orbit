import Combine
import Foundation

protocol ReviewWorkspaceLifecycleControlling {
    func createWorkspace(
        for pullRequest: ResolvedPullRequest,
        workspaceID: UUID,
        now: Date
    ) throws -> ReviewWorkspaceRecord
    func restoreWorkspaces(now: Date) throws -> ReviewWorkspaceReconciliationResult
    func cleanupWorkspace(_ record: ReviewWorkspaceRecord, now: Date) throws -> ReviewWorkspaceCleanupResult
}

struct ReviewWorkspaceLifecycleController: ReviewWorkspaceLifecycleControlling {
    private let store: ReviewWorkspaceMetadataStore
    private let service: ReviewWorkspaceGitService

    init(store: ReviewWorkspaceMetadataStore, service: ReviewWorkspaceGitService) {
        self.store = store
        self.service = service
    }

    func createWorkspace(
        for pullRequest: ResolvedPullRequest,
        workspaceID: UUID,
        now: Date
    ) throws -> ReviewWorkspaceRecord {
        let record = try service.createWorkspace(for: pullRequest, workspaceID: workspaceID, now: now)
        try store.upsert(record)
        return record
    }

    func restoreWorkspaces(now: Date) throws -> ReviewWorkspaceReconciliationResult {
        let records = try store.load()
        let result = try service.reconcile(records, now: now)
        try store.save(result.records)
        return result
    }

    func cleanupWorkspace(_ record: ReviewWorkspaceRecord, now: Date) throws -> ReviewWorkspaceCleanupResult {
        let result = service.cleanupWorkspace(record, now: now)
        switch result {
        case .removed:
            try store.remove(workspaceID: record.id)
        case .cleanupRequired(let updated):
            try store.upsert(updated)
        }
        return result
    }
}

struct ReviewWorkspace: Identifiable, Equatable {
    enum State: Equatable {
        case preparing
        case available
        case running
        case terminating
        case exited(Int32)
        case missing(String)
        case cleanupRequired(String)
        case failed(String)
    }

    // swiftlint:disable:next identifier_name
    let id: UUID
    var displayName: String
    var state: State
    var record: ReviewWorkspaceRecord?

    var paneName: String { "review-workspace:\(id.uuidString.lowercased())" }
}

@MainActor
final class ReviewWorkspaceManager: ObservableObject {
    @Published private(set) var workspaces: [ReviewWorkspace] = []
    private let terminalManager: TerminalManager
    private let lifecycleController: ReviewWorkspaceLifecycleControlling?
    private let now: () -> Date

    init(
        terminalManager: TerminalManager,
        lifecycleController: ReviewWorkspaceLifecycleControlling? = nil,
        now: @escaping () -> Date = Date.init
    ) {
        self.terminalManager = terminalManager
        self.lifecycleController = lifecycleController
        self.now = now
        restoreManagedWorkspaces()
    }

    func workspace(forPaneName paneName: String) -> ReviewWorkspace? {
        workspaces.first { $0.paneName == paneName }
    }

    @discardableResult
    func createFixtureWorkspace(named name: String, workspaceID: UUID = UUID()) -> ReviewWorkspace? {
        let workspace = ReviewWorkspace(id: workspaceID, displayName: name, state: .preparing, record: nil)
        guard terminalManager.reserveWorkspacePane(workspace.paneName) else { return nil }
        workspaces.append(workspace)
        return workspace
    }

    @discardableResult
    func attachManagedWorkspace(
        _ record: ReviewWorkspaceRecord,
        displayName: String,
        initialState: ReviewWorkspace.State? = nil
    ) -> ReviewWorkspace? {
        let initialState = initialState ?? restoredState(for: record)
        let workspace = ReviewWorkspace(
            id: record.id, displayName: displayName, state: initialState, record: record)
        guard terminalManager.reserveWorkspacePane(workspace.paneName) else { return nil }
        workspaces.append(workspace)
        return workspace
    }

    @discardableResult
    func createManagedWorkspace(
        for pullRequest: ResolvedPullRequest,
        displayName: String,
        workspaceID: UUID = UUID()
    ) -> ReviewWorkspace? {
        guard let lifecycleController else { return nil }
        do {
            let record = try lifecycleController.createWorkspace(
                for: pullRequest, workspaceID: workspaceID, now: now())
            return attachManagedWorkspace(record, displayName: displayName, initialState: .preparing)
        } catch {
            return nil
        }
    }

    func install(_ session: TerminalProcessSession, for workspaceID: UUID) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }), workspaces[index].state == .preparing,
            terminalManager.installWorkspaceSession(session, for: workspaces[index].paneName)
        else { return }
        workspaces[index].state = .running
    }

    func requestTermination(for workspaceID: UUID) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }),
            workspaces[index].state == .running
        else {
            return
        }
        workspaces[index].state = .terminating
        terminalManager.terminateWorkspacePane(workspaces[index].paneName)
    }

    func reportTerminalExit(for workspaceID: UUID, exitCode: Int32?) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }) else { return }
        switch workspaces[index].state {
        case .preparing, .available, .running, .terminating: break
        case .exited, .missing, .cleanupRequired, .failed: return
        }
        terminalManager.workspaceProcessTerminated(workspaces[index].paneName, exitCode: exitCode)
        guard let record = workspaces[index].record, workspaces[index].state == .terminating, let lifecycleController
        else {
            workspaces[index].state = .exited(exitCode ?? -1)
            return
        }
        do {
            let cleanup = try lifecycleController.cleanupWorkspace(record, now: now())
            switch cleanup {
            case .removed:
                workspaces[index].record = nil
                workspaces[index].state = .exited(exitCode ?? -1)
            case .cleanupRequired(let updated):
                workspaces[index].record = updated
                workspaces[index].state = .cleanupRequired("Cleanup required for \(updated.worktreePath.path).")
            }
        } catch {
            workspaces[index].state = .cleanupRequired("Cleanup required for \(record.worktreePath.path).")
        }
    }

    func reportSetupFailure(for workspaceID: UUID, message: String) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }) else { return }
        switch workspaces[index].state {
        case .preparing, .running: break
        case .available, .terminating, .exited, .missing, .cleanupRequired, .failed: return
        }
        workspaces[index].state = .failed(message)
    }

    func reportCleanupRequired(for workspaceID: UUID, message: String, record: ReviewWorkspaceRecord? = nil) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }) else { return }
        terminalManager.workspaceProcessTerminated(workspaces[index].paneName, exitCode: nil)
        if let record {
            workspaces[index].record = record
        }
        workspaces[index].state = .cleanupRequired(message)
    }

    func dismiss(_ workspaceID: UUID) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }) else { return }
        switch workspaces[index].state {
        case .available, .exited, .missing, .cleanupRequired, .failed: break
        default: return
        }
        terminalManager.releaseWorkspacePane(workspaces[index].paneName)
        workspaces.remove(at: index)
    }

    private func restoreManagedWorkspaces() {
        guard let lifecycleController else { return }
        do {
            let result = try lifecycleController.restoreWorkspaces(now: now())
            for record in result.records {
                let displayName = "PR #\(record.pullRequestNumber)"
                _ = attachManagedWorkspace(record, displayName: displayName)
            }
            for orphaned in result.orphanedWorktrees {
                let workspaceID = UUID()
                let workspace = ReviewWorkspace(
                    id: workspaceID,
                    displayName: orphaned.worktreePath.lastPathComponent,
                    state: .cleanupRequired("Orphaned managed worktree at \(orphaned.worktreePath.path)."),
                    record: nil)
                guard terminalManager.reserveWorkspacePane(workspace.paneName) else { continue }
                workspaces.append(workspace)
            }
        } catch {}
    }

    private func restoredState(for record: ReviewWorkspaceRecord) -> ReviewWorkspace.State {
        switch record.state {
        case .active:
            .available
        case .cleanupRequired:
            .cleanupRequired("Cleanup required for \(record.worktreePath.path).")
        case .missing:
            .missing("Managed worktree missing at \(record.worktreePath.path).")
        }
    }
}
