import Foundation
import SwiftUI

enum TerminalColorSchemePreference: String, CaseIterable, Codable, Sendable {
    case system
    case light
    case dark

    var label: String {
        switch self {
        case .system:
            "Follow System"
        case .light:
            "Light"
        case .dark:
            "Dark"
        }
    }
}

struct OrbitCockpitSettings: Equatable, Codable, Sendable {
    struct Terminal: Equatable, Codable, Sendable {
        var fontSize: Double = 12
        var usesNerdFont: Bool = true
        var useBrightColorsForBoldText: Bool = true
        var useCustomBlockGlyphs: Bool = true
        var antiAliasCustomBlockGlyphs: Bool = false
    }

    struct Appearance: Equatable, Codable, Sendable {
        var terminalColorSchemePreference: TerminalColorSchemePreference = .system
        var showDebugLogsByDefault: Bool = false
    }

    struct LinksAndInput: Equatable, Codable, Sendable {
        var openLinksDirectly: Bool = true
        var optionKeySendsMeta: Bool = true
        var mouseReportingEnabled: Bool = true
        var backspaceSendsControlH: Bool = false
    }

    struct Advanced: Equatable, Codable, Sendable {
        var preferGPURenderer: Bool = true
        var scrollbackLineLimit: Int = 10_000
    }

    var terminal: Terminal = .init()
    var appearance: Appearance = .init()
    var linksAndInput: LinksAndInput = .init()
    var advanced: Advanced = .init()

    static let defaults = OrbitCockpitSettings()
}

struct TerminalSessionSettings: Equatable, Sendable {
    var fontSize: Double
    var usesNerdFont: Bool
    var useBrightColorsForBoldText: Bool
    var useCustomBlockGlyphs: Bool
    var antiAliasCustomBlockGlyphs: Bool
    var colorSchemePreference: TerminalColorSchemePreference
    var optionKeySendsMeta: Bool
    var mouseReportingEnabled: Bool
    var backspaceSendsControlH: Bool

    static let defaults = TerminalSessionSettings(
        fontSize: OrbitCockpitSettings.defaults.terminal.fontSize,
        usesNerdFont: OrbitCockpitSettings.defaults.terminal.usesNerdFont,
        useBrightColorsForBoldText: OrbitCockpitSettings.defaults.terminal.useBrightColorsForBoldText,
        useCustomBlockGlyphs: OrbitCockpitSettings.defaults.terminal.useCustomBlockGlyphs,
        antiAliasCustomBlockGlyphs: OrbitCockpitSettings.defaults.terminal.antiAliasCustomBlockGlyphs,
        colorSchemePreference: OrbitCockpitSettings.defaults.appearance.terminalColorSchemePreference,
        optionKeySendsMeta: OrbitCockpitSettings.defaults.linksAndInput.optionKeySendsMeta,
        mouseReportingEnabled: OrbitCockpitSettings.defaults.linksAndInput.mouseReportingEnabled,
        backspaceSendsControlH: OrbitCockpitSettings.defaults.linksAndInput.backspaceSendsControlH)
}

extension OrbitCockpitSettings {
    /// The explicit subset of terminal settings that Orbit Cockpit supports
    /// both for new sessions and live application to running SwiftTerm views.
    var terminalSessionSettings: TerminalSessionSettings {
        TerminalSessionSettings(
            fontSize: terminal.fontSize,
            usesNerdFont: terminal.usesNerdFont,
            useBrightColorsForBoldText: terminal.useBrightColorsForBoldText,
            useCustomBlockGlyphs: terminal.useCustomBlockGlyphs,
            antiAliasCustomBlockGlyphs: terminal.antiAliasCustomBlockGlyphs,
            colorSchemePreference: appearance.terminalColorSchemePreference,
            optionKeySendsMeta: linksAndInput.optionKeySendsMeta,
            mouseReportingEnabled: linksAndInput.mouseReportingEnabled,
            backspaceSendsControlH: linksAndInput.backspaceSendsControlH)
    }
}

@MainActor
final class OrbitCockpitSettingsStore: ObservableObject {
    static let defaultStorageKey = "orbit-cockpit.settings"

    @Published private(set) var settings: OrbitCockpitSettings

    private let defaults: UserDefaults
    private let storageKey: String
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    init(
        defaults: UserDefaults = .standard,
        storageKey: String = defaultStorageKey
    ) {
        self.defaults = defaults
        self.storageKey = storageKey
        self.settings = Self.loadSettings(from: defaults, storageKey: storageKey)
    }

    var terminalSessionSettings: TerminalSessionSettings {
        settings.terminalSessionSettings
    }

    func binding<Value>(_ keyPath: WritableKeyPath<OrbitCockpitSettings, Value>) -> Binding<Value> {
        Binding(
            get: { self.settings[keyPath: keyPath] },
            set: { newValue in
                var updated = self.settings
                updated[keyPath: keyPath] = newValue
                self.apply(updated)
            })
    }

    func resetToDefaults() {
        apply(.defaults)
    }

    private func apply(_ updated: OrbitCockpitSettings) {
        settings = updated
        persist(updated)
    }

    private func persist(_ settings: OrbitCockpitSettings) {
        guard let data = try? encoder.encode(settings) else { return }
        defaults.set(data, forKey: storageKey)
    }

    private static func loadSettings(from defaults: UserDefaults, storageKey: String) -> OrbitCockpitSettings {
        guard
            let data = defaults.data(forKey: storageKey),
            let settings = try? JSONDecoder().decode(OrbitCockpitSettings.self, from: data)
        else {
            return .defaults
        }
        return settings
    }
}
