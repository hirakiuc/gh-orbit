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
        store.binding(\.advanced.scrollbackLineLimit).wrappedValue = 20_000

        let reloaded = OrbitCockpitSettingsStore(defaults: defaults)

        #expect(reloaded.settings.terminal.fontSize == 14)
        #expect(reloaded.settings.appearance.showDebugLogsByDefault)
        #expect(reloaded.settings.advanced.scrollbackLineLimit == 20_000)
    }

    @Test("Reset to defaults restores canonical values")
    func testResetToDefaults() throws {
        let defaults = try #require(UserDefaults(suiteName: "OrbitCockpitTests.Settings.\(UUID().uuidString)"))
        let store = OrbitCockpitSettingsStore(defaults: defaults)

        store.binding(\.terminal.fontSize).wrappedValue = 18
        store.binding(\.linksAndInput.optionKeySendsMeta).wrappedValue = false
        store.binding(\.advanced.preferGPURenderer).wrappedValue = false

        store.resetToDefaults()

        #expect(store.settings == .defaults)

        let reloaded = OrbitCockpitSettingsStore(defaults: defaults)
        #expect(reloaded.settings == .defaults)
    }
}
