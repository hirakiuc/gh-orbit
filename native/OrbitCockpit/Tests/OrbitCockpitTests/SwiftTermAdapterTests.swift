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
        let adapter = SwiftTermAdapter(onLog: nil)

        // Access the internal terminalView via the public view property

        guard let terminalView = adapter.view as? LocalProcessTerminalView else {
            Issue.record("adapter.view is not a LocalProcessTerminalView")
            return
        }

        // Verify font size is 12
        #expect(terminalView.font.pointSize == 12)
    }

    @Test("Configured font size is applied to terminal view")
    @MainActor
    func testConfiguredFontSizeIsApplied() async throws {
        let settings = TerminalSessionSettings(fontSize: 16, usesNerdFont: false, colorSchemePreference: .system)
        let adapter = SwiftTermAdapter(settings: settings, onLog: nil)

        guard let terminalView = adapter.view as? LocalProcessTerminalView else {
            Issue.record("adapter.view is not a LocalProcessTerminalView")
            return
        }

        #expect(terminalView.font.pointSize == 16)
    }
}
