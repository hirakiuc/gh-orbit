import Combine
import Foundation

struct ReviewWorkspace: Identifiable, Equatable {
    enum State: Equatable {
        case preparing
        case running
        case terminating
        case exited(Int32)
        case failed(String)
    }

    // swiftlint:disable:next identifier_name
    let id: UUID
    var displayName: String
    var state: State

    var paneName: String { "review-workspace:\(id.uuidString.lowercased())" }
}

@MainActor
final class ReviewWorkspaceManager: ObservableObject {
    @Published private(set) var workspaces: [ReviewWorkspace] = []
    private let terminalManager: TerminalManager

    init(terminalManager: TerminalManager) { self.terminalManager = terminalManager }

    func workspace(forPaneName paneName: String) -> ReviewWorkspace? {
        workspaces.first { $0.paneName == paneName }
    }

    @discardableResult
    func createFixtureWorkspace(named name: String, workspaceID: UUID = UUID()) -> ReviewWorkspace? {
        let workspace = ReviewWorkspace(id: workspaceID, displayName: name, state: .preparing)
        guard terminalManager.reserveWorkspacePane(workspace.paneName) else { return nil }
        workspaces.append(workspace)
        return workspace
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
        case .preparing, .running, .terminating: break
        case .exited, .failed: return
        }
        terminalManager.workspaceProcessTerminated(workspaces[index].paneName, exitCode: exitCode)
        workspaces[index].state = .exited(exitCode ?? -1)
    }

    func reportSetupFailure(for workspaceID: UUID, message: String) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }) else { return }
        switch workspaces[index].state {
        case .preparing, .running: break
        case .terminating, .exited, .failed: return
        }
        workspaces[index].state = .failed(message)
    }

    func dismiss(_ workspaceID: UUID) {
        guard let index = workspaces.firstIndex(where: { $0.id == workspaceID }) else { return }
        switch workspaces[index].state {
        case .exited, .failed: break
        default: return
        }
        terminalManager.releaseWorkspacePane(workspaces[index].paneName)
        workspaces.remove(at: index)
    }
}
