import Testing

@testable import OrbitCockpit

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
}
