import Combine
import Foundation
import Testing

@testable import OrbitCockpit

@Suite("Terminal Manager Tests")
struct TerminalManagerTests {

    @Test("Manager initial state")
    @MainActor
    func testInitialization() async throws {
        let manager = TerminalManager()
        #expect(manager.engines.isEmpty)
    }

    @Test("Engine mapping preservation")
    @MainActor
    func testEngineStorage() async throws {
        let manager = TerminalManager()
        let mockEngine = SwiftTermAdapter()

        manager.engines["TUI"] = mockEngine
        let stored = try #require(manager.engines["TUI"])
        #expect(stored === mockEngine)
    }

    @Test("Nested State Propagation")
    @MainActor
    func testNestedStatePropagation() async throws {
        let manager = TerminalManager()
        var didFire = false

        let cancellable = manager.objectWillChange.sink { _ in
            didFire = true
        }

        manager.engineManager.engineLog = "Test log entry"

        // Yield to allow Combine to process the event
        try await Task.sleep(nanoseconds: 10_000_000)

        #expect(didFire)
        cancellable.cancel()
    }
}
