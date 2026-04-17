import Testing
import Foundation
@testable import OrbitCockpit

@Suite("Terminal Manager Tests")
struct TerminalManagerTests {
    
    @Test("Manager initial state")
    func testInitialization() async throws {
        let manager = TerminalManager()
        #expect(manager.engines.isEmpty)
    }
    
    @Test("Engine mapping preservation")
    func testEngineStorage() async throws {
        let manager = TerminalManager()
        let mockEngine = SwiftTermAdapter()
        
        manager.engines["TUI"] = mockEngine
        #expect(manager.engines["TUI"] === mockEngine)
    }
}
