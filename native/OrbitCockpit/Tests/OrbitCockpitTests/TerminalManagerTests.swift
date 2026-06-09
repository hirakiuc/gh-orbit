import AppKit
import Combine
import Foundation
import Testing

@testable import OrbitCockpit

@MainActor
final class MockTerminalEngine: OrbitTerminalEngine {
    let view = NSView()
    var isDarkModeCalls: [Bool] = []

    func feed(data: Data) {}

    func send(string: String) {}

    func resize(cols: Int, rows: Int) {}

    func getBuffer() -> String {
        ""
    }

    func isDarkMode(_ isDark: Bool) {
        isDarkModeCalls.append(isDark)
    }
}

@MainActor
final class MockTerminalSession: TerminalProcessSession {
    let engine: OrbitTerminalEngine
    var terminateCalls = 0

    init(engine: OrbitTerminalEngine = MockTerminalEngine()) {
        self.engine = engine
    }

    func terminateProcess() {
        terminateCalls += 1
    }
}

@Suite("Terminal Manager Tests")
struct TerminalManagerTests {

    @Test("Manager initial state")
    @MainActor
    func testInitialization() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        #expect(manager.state(for: "TUI") == nil)
        #expect(manager.engine(for: "TUI") == nil)
    }

    @Test("Running session exposes engine")
    @MainActor
    func testRunningSessionExposesEngine() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        let mockEngine = MockTerminalEngine()
        let mockSession = MockTerminalSession(engine: mockEngine)

        manager.installSession(mockSession, for: "TUI")

        let stored = try #require(manager.engine(for: "TUI"))
        #expect(stored === mockEngine)
        #expect(manager.state(for: "TUI") == .running)
    }

    @Test("Process termination marks pane exited")
    @MainActor
    func testProcessTerminationMarksPaneExited() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        let mockSession = MockTerminalSession()

        manager.installSession(mockSession, for: "TUI")
        manager.engineManager?.isEngineReady = true
        manager.processTerminated(name: "TUI", exitCode: 0)

        #expect(manager.state(for: "TUI") == .exited(exitCode: 0))
        #expect(manager.engine(for: "TUI") == nil)
        #expect(manager.engineManager?.isEngineReady == true)
        #expect(mockSession.terminateCalls == 0)
    }

    @Test("Shutdown terminates running pane processes")
    @MainActor
    func testShutdownTerminatesRunningPaneProcesses() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        let runningSession = MockTerminalSession()
        let exitedSession = MockTerminalSession()

        manager.installSession(runningSession, for: "TUI")
        manager.installSession(exitedSession, for: "Agent Alpha", state: .exited(exitCode: 0))
        manager.engineManager?.isEngineReady = true

        manager.shutdown()

        #expect(runningSession.terminateCalls == 1)
        #expect(exitedSession.terminateCalls == 0)
        #expect(manager.engineManager?.isEngineReady == false)
    }

    @Test("Nested State Propagation")
    @MainActor
    func testNestedStatePropagation() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)

        var didFire = false

        let cancellable = manager.objectWillChange.sink { _ in
            didFire = true
        }

        manager.engineManager?.isEngineReady = true

        // Yield to allow Combine to process the event
        try await Task.sleep(nanoseconds: 10_000_000)

        #expect(didFire)
        cancellable.cancel()
    }

    @Test("Shutdown clears ready state")
    @MainActor
    func testShutdownClearsEngineReadiness() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)

        manager.engineManager?.isEngineReady = true
        manager.shutdown()

        #expect(manager.engineManager?.isEngineReady == false)
    }
}
