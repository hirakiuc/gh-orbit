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
    }

    struct Appearance: Equatable, Codable, Sendable {
        var terminalColorSchemePreference: TerminalColorSchemePreference = .system
        var showDebugLogsByDefault: Bool = false
    }

    struct LinksAndInput: Equatable, Codable, Sendable {
        var openLinksDirectly: Bool = true
        var optionKeySendsMeta: Bool = true
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
    var colorSchemePreference: TerminalColorSchemePreference

    static let defaults = TerminalSessionSettings(
        fontSize: OrbitCockpitSettings.defaults.terminal.fontSize,
        usesNerdFont: OrbitCockpitSettings.defaults.terminal.usesNerdFont,
        colorSchemePreference: OrbitCockpitSettings.defaults.appearance.terminalColorSchemePreference)
}

extension OrbitCockpitSettings {
    var terminalSessionSettings: TerminalSessionSettings {
        TerminalSessionSettings(
            fontSize: terminal.fontSize,
            usesNerdFont: terminal.usesNerdFont,
            colorSchemePreference: appearance.terminalColorSchemePreference)
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
