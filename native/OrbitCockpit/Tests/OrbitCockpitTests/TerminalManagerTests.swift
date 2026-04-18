import XCTest
import Foundation
@testable import OrbitCockpit

final class TerminalManagerTests: XCTestCase {
    
    @MainActor
    func testInitialization() async throws {
        let manager = TerminalManager()
        XCTAssertTrue(manager.engines.isEmpty)
    }
    
    @MainActor
    func testEngineStorage() async throws {
        let manager = TerminalManager()
        let mockEngine = SwiftTermAdapter()
        
        manager.engines["TUI"] = mockEngine
        XCTAssertTrue(manager.engines["TUI"] === mockEngine)
    }
}
