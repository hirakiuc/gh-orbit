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
    var diagnostics: [WorkspaceDiagnosticEntry] = []

    var paneName: String { "review-workspace:\(id.uuidString.lowercased())" }
}

struct WorkspaceDiagnosticEntry: Identifiable, Equatable {
    enum Category: String, Equatable {
        case launch = "Launch"
        case resolution = "Resolution"
        case workspace = "Workspace"
        case codex = "Codex"
        case cleanup = "Cleanup"
        case restore = "Restore"
    }

    // swiftlint:disable:next identifier_name
    let id = UUID()
    let timestamp: Date
    let category: Category
    let level: LogLevel
    let message: String
}

struct NativeReviewRequest: Identifiable, Equatable {
    let repository: RepositoryIdentity
    let pullRequestNumber: Int
    let title: String
    let subtitle: String

    // swiftlint:disable:next identifier_name
    var id: String { paneName }
    var paneName: String {
        "review-request:\(repository.host)/\(repository.owner)/\(repository.name)#\(pullRequestNumber)"
    }
    var displayName: String { "PR #\(pullRequestNumber)" }
    var repositoryLabel: String { "\(repository.owner)/\(repository.name)" }
}

enum ReviewWorkspaceLaunchError: Error, Equatable, LocalizedError {
    case unavailableResolver
    case unavailableLifecycle
    case duplicatePlaceholderConflict

    var errorDescription: String? {
        switch self {
        case .unavailableResolver:
            "Pull request resolution is unavailable in this build."
        case .unavailableLifecycle:
            "Managed review-workspace lifecycle is unavailable in this build."
        case .duplicatePlaceholderConflict:
            "A review workspace placeholder already exists but could not be reused safely."
        }
    }
}

@MainActor
final class ReviewWorkspaceManager: ObservableObject {
    @Published private(set) var workspaces: [ReviewWorkspace] = []
    private let terminalManager: TerminalManager
    private let lifecycleController: ReviewWorkspaceLifecycleControlling?
    private let codexLauncher: ReviewWorkspaceCodexLaunching?
    private let pullRequestResolverFactory: () throws -> any PullRequestResolving
    private let now: () -> Date
    private var didRestoreManagedWorkspaces = false
    private var phaseOneClaims: [LaunchClaimKey: UUID] = [:]
    var onPaneFocusRequested: ((String) -> Void)?

    init(
        terminalManager: TerminalManager,
        lifecycleController: ReviewWorkspaceLifecycleControlling? = nil,
        codexLauncher: ReviewWorkspaceCodexLaunching? = nil,
        pullRequestResolverFactory: @escaping () throws -> any PullRequestResolving = {
            try PullRequestResolver.production()
        },
        now: @escaping () -> Date = Date.init
    ) {
        self.terminalManager = terminalManager
        self.lifecycleController = lifecycleController
        self.codexLauncher = codexLauncher
        self.pullRequestResolverFactory = pullRequestResolverFactory
        self.now = now
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
    func startReviewWorkspace(for request: NativeReviewRequest) -> UUID? {
        let claimKey = LaunchClaimKey(repository: request.repository, pullRequestNumber: request.pullRequestNumber)
        if let existingID = activeClaimedWorkspaceID(for: claimKey) {
            appendDiagnostic(
                for: existingID,
                category: .launch,
                level: .info,
                message: "Reused the existing in-progress workspace for PR #\(request.pullRequestNumber)."
            )
            requestFocus(for: existingID)
            return existingID
        }

        let workspaceID = UUID()
        guard createFixtureWorkspace(named: request.displayName, workspaceID: workspaceID) != nil else {
            return nil
        }
        phaseOneClaims[claimKey] = workspaceID
        appendDiagnostic(
            for: workspaceID,
            category: .launch,
            level: .info,
            message: "Accepted a start request for \(request.repositoryLabel) PR #\(request.pullRequestNumber)."
        )
        appendDiagnostic(
            for: workspaceID,
            category: .resolution,
            level: .info,
            message: "Resolving the selected pull request against the local clone."
        )
        requestFocus(for: workspaceID)

        let resolver: any PullRequestResolving
        do {
            resolver = try pullRequestResolverFactory()
        } catch {
            phaseOneClaims.removeValue(forKey: claimKey)
            reportSetupFailure(
                for: workspaceID,
                category: .resolution,
                message: error.localizedDescription
            )
            return workspaceID
        }

        let completeReviewWorkspaceStart:
            @MainActor (
                NativeReviewRequest, LaunchClaimKey, UUID, Result<ResolvedPullRequest, Error>
            ) -> Void = { [weak self] request, claimKey, workspaceID, result in
                self?.completeReviewWorkspaceStart(
                    for: request,
                    claimKey: claimKey,
                    placeholderWorkspaceID: workspaceID,
                    result: result
                )
            }

        Task.detached(priority: .userInitiated) {
            let result = Result {
                try resolver.resolve(repository: request.repository, number: request.pullRequestNumber)
            }
            await completeReviewWorkspaceStart(request, claimKey, workspaceID, result)
        }

        return workspaceID
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
            let workspace = attachManagedWorkspace(record, displayName: displayName, initialState: .preparing)
            if let workspace {
                launchCodexIfNeeded(for: workspace.id)
            }
            return workspace
        } catch {
            return nil
        }
    }

    func launchCodexIfNeeded(for workspaceID: UUID) {
        guard let codexLauncher,
            let index = workspaces.firstIndex(where: { $0.id == workspaceID }),
            workspaces[index].state == .preparing,
            let record = workspaces[index].record
        else {
            return
        }

        do {
            let session = try codexLauncher.launchSession(
                for: record,
                environment: terminalManager.managedLaunchEnvironment,
                onTerminate: { [weak self] exitCode in
                    Task { @MainActor in
                        self?.reportTerminalExit(for: workspaceID, exitCode: exitCode)
                    }
                })
            appendDiagnostic(
                for: workspaceID,
                category: .codex,
                level: .info,
                message: "Launching the Codex review session."
            )
            install(session, for: workspaceID)
        } catch {
            reportSetupFailure(
                for: workspaceID,
                category: .codex,
                message: error.localizedDescription
            )
        }
    }

    func install(_ session: TerminalProcessSession, for workspaceID: UUID) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }), workspaces[index].state == .preparing
        else { return }
        guard terminalManager.installWorkspaceSession(session, for: workspaces[index].paneName) else {
            session.terminateProcess()
            reportSetupFailure(
                for: workspaceID,
                category: .codex,
                message: "Failed to attach the managed workspace to a terminal session."
            )
            return
        }
        workspaces[index].state = .running
        appendDiagnostic(
            for: workspaceID,
            category: .codex,
            level: .info,
            message: "Attached the workspace to a running Codex session."
        )
    }

    func requestTermination(for workspaceID: UUID) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }),
            workspaces[index].state == .running
        else {
            return
        }
        workspaces[index].state = .terminating
        appendDiagnostic(
            for: workspaceID,
            category: .cleanup,
            level: .info,
            message: "Terminating the active workspace session."
        )
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
            appendDiagnostic(
                for: workspaceID,
                category: .cleanup,
                level: .info,
                message: "The workspace session exited with code \(exitCode ?? -1)."
            )
            return
        }
        do {
            let cleanup = try lifecycleController.cleanupWorkspace(record, now: now())
            switch cleanup {
            case .removed:
                workspaces[index].record = nil
                workspaces[index].state = .exited(exitCode ?? -1)
                appendDiagnostic(
                    for: workspaceID,
                    category: .cleanup,
                    level: .info,
                    message: "Cleaned up the managed workspace after exit."
                )
            case .cleanupRequired(let updated):
                workspaces[index].record = updated
                workspaces[index].state = .cleanupRequired("Cleanup required for \(updated.worktreePath.path).")
                appendDiagnostic(
                    for: workspaceID,
                    category: .cleanup,
                    level: .warning,
                    message: "Workspace cleanup requires manual action for \(updated.worktreePath.path)."
                )
            }
        } catch {
            workspaces[index].state = .cleanupRequired("Cleanup required for \(record.worktreePath.path).")
            appendDiagnostic(
                for: workspaceID,
                category: .cleanup,
                level: .error,
                message: "Workspace cleanup failed for \(record.worktreePath.path)."
            )
        }
    }

    func reportSetupFailure(
        for workspaceID: UUID,
        category: WorkspaceDiagnosticEntry.Category = .workspace,
        message: String
    ) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }) else { return }
        switch workspaces[index].state {
        case .preparing, .running: break
        case .available, .terminating, .exited, .missing, .cleanupRequired, .failed: return
        }
        appendDiagnostic(
            for: workspaceID,
            category: category,
            level: .error,
            message: message
        )
        workspaces[index].state = .failed(message)
    }

    func reportCleanupRequired(for workspaceID: UUID, message: String, record: ReviewWorkspaceRecord? = nil) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }) else { return }
        terminalManager.workspaceProcessTerminated(workspaces[index].paneName, exitCode: nil)
        if let record {
            workspaces[index].record = record
        }
        appendDiagnostic(
            for: workspaceID,
            category: .cleanup,
            level: .warning,
            message: message
        )
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

    func restoreManagedWorkspacesIfNeeded() {
        guard !didRestoreManagedWorkspaces else { return }
        didRestoreManagedWorkspaces = true
        restoreManagedWorkspaces()
    }

    private func completeReviewWorkspaceStart(
        for request: NativeReviewRequest,
        claimKey: LaunchClaimKey,
        placeholderWorkspaceID: UUID,
        result: Result<ResolvedPullRequest, Error>
    ) {
        phaseOneClaims.removeValue(forKey: claimKey)

        switch result {
        case .success(let pullRequest):
            appendDiagnostic(
                for: placeholderWorkspaceID,
                category: .resolution,
                level: .info,
                message: "Resolved the pull request head at \(pullRequest.headSHA)."
            )
            reconcileResolvedWorkspace(
                for: request,
                pullRequest: pullRequest,
                placeholderWorkspaceID: placeholderWorkspaceID
            )
        case .failure(let error):
            reportSetupFailure(
                for: placeholderWorkspaceID,
                category: .resolution,
                message: error.localizedDescription
            )
        }
    }

    private func reconcileResolvedWorkspace(
        for request: NativeReviewRequest,
        pullRequest: ResolvedPullRequest,
        placeholderWorkspaceID: UUID
    ) {
        let resolvedKey = ResolvedLaunchKey(
            repository: request.repository,
            pullRequestNumber: request.pullRequestNumber,
            headSHA: pullRequest.headSHA
        )
        if let existingID = activeResolvedWorkspaceID(for: resolvedKey), existingID != placeholderWorkspaceID {
            appendDiagnostic(
                for: existingID,
                category: .workspace,
                level: .info,
                message: "Reused the existing workspace for the resolved head \(pullRequest.headSHA)."
            )
            discardPlaceholderWorkspace(for: placeholderWorkspaceID)
            requestFocus(for: existingID)
            return
        }

        do {
            try materializeManagedWorkspace(
                for: pullRequest,
                displayName: request.displayName,
                workspaceID: placeholderWorkspaceID
            )
            appendDiagnostic(
                for: placeholderWorkspaceID,
                category: .workspace,
                level: .info,
                message: "Prepared the managed worktree for \(request.displayName)."
            )
            requestFocus(for: placeholderWorkspaceID)
        } catch {
            reportSetupFailure(
                for: placeholderWorkspaceID,
                category: .workspace,
                message: error.localizedDescription
            )
        }
    }

    private func materializeManagedWorkspace(
        for pullRequest: ResolvedPullRequest,
        displayName: String,
        workspaceID: UUID
    ) throws {
        guard let lifecycleController else {
            throw ReviewWorkspaceLaunchError.unavailableLifecycle
        }
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }),
            workspaces[index].record == nil,
            workspaces[index].state == .preparing
        else {
            throw ReviewWorkspaceLaunchError.duplicatePlaceholderConflict
        }

        let record = try lifecycleController.createWorkspace(
            for: pullRequest,
            workspaceID: workspaceID,
            now: now()
        )
        workspaces[index].displayName = displayName
        workspaces[index].record = record
        launchCodexIfNeeded(for: workspaceID)
    }

    private func discardPlaceholderWorkspace(for workspaceID: UUID) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }),
            workspaces[index].record == nil
        else { return }
        terminalManager.releaseWorkspacePane(workspaces[index].paneName)
        workspaces.remove(at: index)
    }

    private func activeClaimedWorkspaceID(for key: LaunchClaimKey) -> UUID? {
        guard let workspaceID = phaseOneClaims[key] else { return nil }
        guard let workspace = workspaces.first(where: { $0.id == workspaceID }), isDuplicateActiveState(workspace.state)
        else {
            phaseOneClaims.removeValue(forKey: key)
            return nil
        }
        return workspace.id
    }

    private func activeResolvedWorkspaceID(for key: ResolvedLaunchKey) -> UUID? {
        workspaces.first {
            guard let record = $0.record else { return false }
            return $0.record?.repository == key.repository
                && record.pullRequestNumber == key.pullRequestNumber
                && record.headSHA.caseInsensitiveCompare(key.headSHA) == .orderedSame
                && isDuplicateActiveState($0.state)
        }?.id
    }

    private func isDuplicateActiveState(_ state: ReviewWorkspace.State) -> Bool {
        switch state {
        case .preparing, .running:
            true
        case .available, .terminating, .exited, .missing, .cleanupRequired, .failed:
            false
        }
    }

    private func requestFocus(for workspaceID: UUID) {
        guard let workspace = workspaces.first(where: { $0.id == workspaceID }) else { return }
        onPaneFocusRequested?(workspace.paneName)
    }

    private func appendDiagnostic(
        for workspaceID: UUID,
        category: WorkspaceDiagnosticEntry.Category,
        level: LogLevel,
        message: String
    ) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }) else { return }
        workspaces[index].diagnostics.append(
            WorkspaceDiagnosticEntry(
                timestamp: now(),
                category: category,
                level: level,
                message: message
            )
        )
    }

    private func restoreManagedWorkspaces() {
        guard let lifecycleController else { return }
        do {
            let result = try lifecycleController.restoreWorkspaces(now: now())
            for record in result.records {
                let displayName = "PR #\(record.pullRequestNumber)"
                if let workspace = attachManagedWorkspace(record, displayName: displayName) {
                    appendDiagnostic(
                        for: workspace.id,
                        category: .restore,
                        level: .info,
                        message: "Restored a managed workspace from persisted state."
                    )
                }
            }
            for orphaned in result.orphanedWorktrees {
                let workspaceID = UUID()
                let workspace = ReviewWorkspace(
                    id: workspaceID,
                    displayName: orphaned.worktreePath.lastPathComponent,
                    state: .cleanupRequired("Orphaned managed worktree at \(orphaned.worktreePath.path)."),
                    record: nil,
                    diagnostics: [
                        WorkspaceDiagnosticEntry(
                            timestamp: now(),
                            category: .restore,
                            level: .warning,
                            message: "Detected an orphaned managed worktree at \(orphaned.worktreePath.path)."
                        )
                    ])
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

private struct LaunchClaimKey: Hashable {
    let repository: RepositoryIdentity
    let pullRequestNumber: Int
}

private struct ResolvedLaunchKey: Hashable {
    let repository: RepositoryIdentity
    let pullRequestNumber: Int
    let headSHA: String
}
