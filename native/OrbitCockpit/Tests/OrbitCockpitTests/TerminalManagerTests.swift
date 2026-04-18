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
}
