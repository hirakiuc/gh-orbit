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

enum TerminalCursorStylePreference: String, CaseIterable, Codable, Sendable {
    case blinkBlock
    case steadyBlock
    case blinkUnderline
    case steadyUnderline
    case blinkBar
    case steadyBar

    var label: String {
        switch self {
        case .blinkBlock:
            "Blinking Block"
        case .steadyBlock:
            "Steady Block"
        case .blinkUnderline:
            "Blinking Underline"
        case .steadyUnderline:
            "Steady Underline"
        case .blinkBar:
            "Blinking Bar"
        case .steadyBar:
            "Steady Bar"
        }
    }
}

enum TerminalAnsi256PaletteStrategyPreference: String, CaseIterable, Codable, Sendable {
    case xterm
    case base16Lab

    var label: String {
        switch self {
        case .xterm:
            "xterm"
        case .base16Lab:
            "Base16 LAB"
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
        var cursorStyle: TerminalCursorStylePreference = .blinkBlock
        var termName: String = "xterm-256color"
        var tabWidth: Int = 8
        var screenReaderMode: Bool = false
        var sixelSupportEnabled: Bool = true
        var ansi256PaletteStrategy: TerminalAnsi256PaletteStrategyPreference = .base16Lab
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

struct TerminalStartupSettings: Equatable, Sendable {
    var scrollbackLineLimit: Int
    var cursorStyle: TerminalCursorStylePreference
    var termName: String
    var tabWidth: Int
    var screenReaderMode: Bool
    var sixelSupportEnabled: Bool
    var ansi256PaletteStrategy: TerminalAnsi256PaletteStrategyPreference

    static let defaults = TerminalStartupSettings(
        scrollbackLineLimit: OrbitCockpitSettings.defaults.advanced.scrollbackLineLimit,
        cursorStyle: OrbitCockpitSettings.defaults.advanced.cursorStyle,
        termName: OrbitCockpitSettings.defaults.advanced.termName,
        tabWidth: OrbitCockpitSettings.defaults.advanced.tabWidth,
        screenReaderMode: OrbitCockpitSettings.defaults.advanced.screenReaderMode,
        sixelSupportEnabled: OrbitCockpitSettings.defaults.advanced.sixelSupportEnabled,
        ansi256PaletteStrategy: OrbitCockpitSettings.defaults.advanced.ansi256PaletteStrategy)
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

    /// Startup-only SwiftTerm configuration that applies when creating a new session.
    var terminalStartupSettings: TerminalStartupSettings {
        TerminalStartupSettings(
            scrollbackLineLimit: advanced.scrollbackLineLimit,
            cursorStyle: advanced.cursorStyle,
            termName: advanced.termName,
            tabWidth: advanced.tabWidth,
            screenReaderMode: advanced.screenReaderMode,
            sixelSupportEnabled: advanced.sixelSupportEnabled,
            ansi256PaletteStrategy: advanced.ansi256PaletteStrategy)
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

    var terminalStartupSettings: TerminalStartupSettings {
        settings.terminalStartupSettings
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
