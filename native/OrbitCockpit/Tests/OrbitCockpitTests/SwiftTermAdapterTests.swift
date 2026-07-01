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
        let settings = TerminalSessionSettings(
            fontSize: 16,
            usesNerdFont: false,
            useBrightColorsForBoldText: true,
            useCustomBlockGlyphs: true,
            antiAliasCustomBlockGlyphs: false,
            colorSchemePreference: .system,
            optionKeySendsMeta: true,
            mouseReportingEnabled: true,
            backspaceSendsControlH: false)
        let adapter = SwiftTermAdapter(settings: settings, onLog: nil)

        guard let terminalView = adapter.view as? LocalProcessTerminalView else {
            Issue.record("adapter.view is not a LocalProcessTerminalView")
            return
        }

        #expect(terminalView.font.pointSize == 16)
    }

    @Test("Live-applied SwiftTerm settings update an existing terminal view")
    @MainActor
    func testApplyTerminalSettingsUpdatesExistingView() async throws {
        let adapter = SwiftTermAdapter(onLog: nil)

        guard let terminalView = adapter.view as? LocalProcessTerminalView else {
            Issue.record("adapter.view is not a LocalProcessTerminalView")
            return
        }

        adapter.applyTerminalSettings(
            TerminalSessionSettings(
                fontSize: 15,
                usesNerdFont: false,
                useBrightColorsForBoldText: false,
                useCustomBlockGlyphs: false,
                antiAliasCustomBlockGlyphs: true,
                colorSchemePreference: .light,
                optionKeySendsMeta: false,
                mouseReportingEnabled: false,
                backspaceSendsControlH: true),
            isDark: true)

        #expect(terminalView.font.pointSize == 15)
        #expect(!terminalView.optionAsMetaKey)
        #expect(!terminalView.allowMouseReporting)
        #expect(terminalView.backspaceSendsControlH)
        #expect(!terminalView.useBrightColors)
        #expect(!terminalView.customBlockGlyphs)
        #expect(terminalView.antiAliasCustomBlockGlyphs)
        #expect(terminalView.nativeBackgroundColor == .white)
        #expect(terminalView.nativeForegroundColor == .black)
    }
}
