import AppKit
import Foundation
import SwiftTerm
import Testing

@testable import OrbitCockpit

@Suite("SwiftTermAdapter Tests")
struct SwiftTermAdapterTests {

    @Test("Font initialization")
    @MainActor
    func testFontInitialization() async throws {
        let adapter = SwiftTermAdapter()

        // Access the internal terminalView via the public view property
        guard let terminalView = adapter.view as? LocalProcessTerminalView else {
            Issue.record("adapter.view is not a LocalProcessTerminalView")
            return
        }

        // Verify font size is 12
        #expect(terminalView.font.pointSize == 12)
    }
}
