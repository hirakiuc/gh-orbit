import AppKit
import Foundation
import SwiftTerm
import Testing

@testable import OrbitCockpit

private final class StubRendererController: TerminalRendererControlling {
    var metalBufferingMode: MetalBufferingMode = .perRowPersistent
    var isUsingMetalRenderer = false
    var setUseMetalCalls: [Bool] = []
    var errorToThrow: Error?

    func setUseMetal(_ enabled: Bool) throws {
        setUseMetalCalls.append(enabled)
        if let errorToThrow {
            throw errorToThrow
        }
        isUsingMetalRenderer = enabled
    }
}

@Suite("SwiftTermAdapter Tests")
struct SwiftTermAdapterTests {

    private static func isSteadyBar(_ style: CursorStyle) -> Bool {
        if case .steadyBar = style {
            return true
        }
        return false
    }

    private static func isXtermPalette(_ strategy: Ansi256PaletteStrategy) -> Bool {
        if case .xterm = strategy {
            return true
        }
        return false
    }

    private static func isPerFrameAggregated(_ mode: MetalBufferingMode) -> Bool {
        if case .perFrameAggregated = mode {
            return true
        }
        return false
    }

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

    @Test("Startup-only SwiftTerm settings are configured on the terminal")
    @MainActor
    func testStartupSettingsAreAppliedOnInitialization() async throws {
        let startupSettings = TerminalStartupSettings(
            scrollbackLineLimit: 20_000,
            cursorStyle: .steadyBar,
            termName: "xterm-gh-orbit",
            tabWidth: 4,
            screenReaderMode: true,
            sixelSupportEnabled: false,
            ansi256PaletteStrategy: .xterm)
        let adapter = SwiftTermAdapter(startupSettings: startupSettings, onLog: nil)

        guard let terminalView = adapter.view as? LocalProcessTerminalView else {
            Issue.record("adapter.view is not a LocalProcessTerminalView")
            return
        }

        let options = terminalView.getTerminal().options
        #expect(options.scrollback == 20_000)
        #expect(Self.isSteadyBar(options.cursorStyle))
        #expect(options.termName == "xterm-gh-orbit")
        #expect(options.tabStopWidth == 4)
        #expect(options.screenReaderMode)
        #expect(!options.enableSixelReported)
        #expect(Self.isXtermPalette(options.ansi256PaletteStrategy))
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

    @Test("Renderer settings apply successfully and report active Metal state")
    @MainActor
    func testApplyRendererSettingsSuccess() async throws {
        let rendererController = StubRendererController()
        let adapter = SwiftTermAdapter(
            rendererSettings: TerminalRendererSettings(
                useMetalRenderer: false,
                metalBufferingMode: .perRowPersistent),
            rendererController: rendererController,
            onLog: nil)

        adapter.applyRendererSettings(
            TerminalRendererSettings(
                useMetalRenderer: true,
                metalBufferingMode: .perFrameAggregated))

        #expect(Self.isPerFrameAggregated(rendererController.metalBufferingMode))
        #expect(rendererController.setUseMetalCalls == [false, true])
        #expect(adapter.rendererStatus.preferredMetalRenderer)
        #expect(adapter.rendererStatus.isUsingMetalRenderer)
        #expect(adapter.rendererStatus.lastRendererError == nil)
    }

    @Test("Renderer settings preserve fallback when Metal activation fails")
    @MainActor
    func testApplyRendererSettingsFallbackOnMetalError() async throws {
        let rendererController = StubRendererController()
        let adapter = SwiftTermAdapter(
            rendererSettings: TerminalRendererSettings(
                useMetalRenderer: false,
                metalBufferingMode: .perRowPersistent),
            rendererController: rendererController,
            onLog: nil)
        rendererController.errorToThrow = MetalError.deviceUnavailable

        adapter.applyRendererSettings(
            TerminalRendererSettings(
                useMetalRenderer: true,
                metalBufferingMode: .perFrameAggregated))

        #expect(Self.isPerFrameAggregated(rendererController.metalBufferingMode))
        #expect(rendererController.setUseMetalCalls == [false, true, false])
        #expect(adapter.rendererStatus.preferredMetalRenderer)
        #expect(!adapter.rendererStatus.isUsingMetalRenderer)
        #expect(adapter.rendererStatus.lastRendererError == MetalError.deviceUnavailable.description)
    }
}
