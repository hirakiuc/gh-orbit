import Foundation
import Testing

@testable import OrbitCockpit

@Suite("OrbitCockpit Settings Store Tests")
@MainActor
struct OrbitCockpitSettingsStoreTests {
    @Test("Defaults load when nothing is persisted")
    func testDefaultsLoad() throws {
        let defaults = try #require(UserDefaults(suiteName: "OrbitCockpitTests.Settings.\(UUID().uuidString)"))
        let store = OrbitCockpitSettingsStore(defaults: defaults)

        #expect(store.settings == .defaults)
    }

    @Test("Persisted values survive store recreation")
    func testPersistedValuesReload() throws {
        let suiteName = "OrbitCockpitTests.Settings.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suiteName))
        let store = OrbitCockpitSettingsStore(defaults: defaults)

        store.binding(\.terminal.fontSize).wrappedValue = 14
        store.binding(\.appearance.showDebugLogsByDefault).wrappedValue = true
        store.binding(\.linksAndInput.optionKeySendsMeta).wrappedValue = false
        store.binding(\.advanced.scrollbackLineLimit).wrappedValue = 20_000
        store.binding(\.advanced.cursorStyle).wrappedValue = .steadyBar
        store.binding(\.advanced.termName).wrappedValue = "xterm-gh-orbit"
        store.binding(\.advanced.tabWidth).wrappedValue = 4
        store.binding(\.advanced.screenReaderMode).wrappedValue = true
        store.binding(\.advanced.sixelSupportEnabled).wrappedValue = false
        store.binding(\.advanced.ansi256PaletteStrategy).wrappedValue = .xterm

        let reloaded = OrbitCockpitSettingsStore(defaults: defaults)

        #expect(reloaded.settings.terminal.fontSize == 14)
        #expect(reloaded.settings.appearance.showDebugLogsByDefault)
        #expect(!reloaded.settings.linksAndInput.optionKeySendsMeta)
        #expect(reloaded.settings.advanced.scrollbackLineLimit == 20_000)
        #expect(reloaded.settings.advanced.cursorStyle == .steadyBar)
        #expect(reloaded.settings.advanced.termName == "xterm-gh-orbit")
        #expect(reloaded.settings.advanced.tabWidth == 4)
        #expect(reloaded.settings.advanced.screenReaderMode)
        #expect(!reloaded.settings.advanced.sixelSupportEnabled)
        #expect(reloaded.settings.advanced.ansi256PaletteStrategy == .xterm)
    }

    @Test("Reset to defaults restores canonical values")
    func testResetToDefaults() throws {
        let defaults = try #require(UserDefaults(suiteName: "OrbitCockpitTests.Settings.\(UUID().uuidString)"))
        let store = OrbitCockpitSettingsStore(defaults: defaults)

        store.binding(\.terminal.fontSize).wrappedValue = 18
        store.binding(\.linksAndInput.optionKeySendsMeta).wrappedValue = false
        store.binding(\.linksAndInput.mouseReportingEnabled).wrappedValue = false
        store.binding(\.advanced.preferGPURenderer).wrappedValue = false

        store.resetToDefaults()

        #expect(store.settings == .defaults)

        let reloaded = OrbitCockpitSettingsStore(defaults: defaults)
        #expect(reloaded.settings == .defaults)
    }
}
