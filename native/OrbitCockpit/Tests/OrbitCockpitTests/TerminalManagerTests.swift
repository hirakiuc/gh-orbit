import Combine
import Foundation
import Testing

@testable import OrbitCockpit

@Suite("Terminal Manager Tests")
struct TerminalManagerTests {

    @Test("Manager initial state")
    @MainActor
    func testInitialization() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        #expect(manager.engines.isEmpty)
    }

    @Test("Engine mapping preservation")
    @MainActor
    func testEngineStorage() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        let mockEngine = SwiftTermAdapter(onLog: nil)

        manager.engines["TUI"] = mockEngine
        let stored = try #require(manager.engines["TUI"])
        #expect(stored === mockEngine)
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
