import Testing
import Foundation
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
        #expect(manager.engines["TUI"] === mockEngine)
    }
}
