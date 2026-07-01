import AppKit
import Combine
import Foundation
import Testing

@testable import OrbitCockpit

@MainActor
final class MockTerminalEngine: OrbitTerminalEngine {
    let view = NSView()
    var isDarkModeCalls: [Bool] = []
    var appliedSettings: [(TerminalSessionSettings, Bool)] = []

    func feed(data: Data) {}

    func send(string: String) {}

    func resize(cols: Int, rows: Int) {}

    func getBuffer() -> String {
        ""
    }

    func applyTerminalSettings(_ settings: TerminalSessionSettings, isDark: Bool) {
        appliedSettings.append((settings, isDark))
    }

    func isDarkMode(_ isDark: Bool) {
        isDarkModeCalls.append(isDark)
    }
}

@MainActor
final class MockTerminalSession: TerminalProcessSession {
    let engine: OrbitTerminalEngine
    var terminateCalls = 0
    var sendCalls: [String] = []

    init(engine: OrbitTerminalEngine = MockTerminalEngine()) {
        self.engine = engine
    }

    func send(string: String) {
        sendCalls.append(string)
    }

    func terminateProcess() {
        terminateCalls += 1
    }
}

@MainActor
final class RecordingSessionLauncher: TerminalSessionLaunching {
    var lastRequest: TerminalLaunchRequest?
    var lastSettings: TerminalSessionSettings?
    var lastStartupSettings: TerminalStartupSettings?
    var lastIsDark: Bool?
    let session: TerminalProcessSession

    init(session: TerminalProcessSession = MockTerminalSession()) {
        self.session = session
    }

    func launchSession(
        request: TerminalLaunchRequest,
        settings: TerminalSessionSettings,
        startupSettings: TerminalStartupSettings,
        isDark: Bool,
        onLog: ((String, LogLevel) -> Void)?,
        onTerminate: @escaping (Int32?) -> Void
    ) -> TerminalProcessSession {
        lastRequest = request
        lastSettings = settings
        lastStartupSettings = startupSettings
        lastIsDark = isDark
        return session
    }
}

@Suite("Terminal Manager Tests")
struct TerminalManagerTests {

    @Test("Manager initial state")
    @MainActor
    func testInitialization() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        #expect(manager.state(for: "TUI") == nil)
        #expect(manager.engine(for: "TUI") == nil)
    }

    @Test("Running session exposes engine")
    @MainActor
    func testRunningSessionExposesEngine() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        let mockEngine = MockTerminalEngine()
        let mockSession = MockTerminalSession(engine: mockEngine)

        manager.installSession(mockSession, for: "TUI")

        let stored = try #require(manager.engine(for: "TUI"))
        #expect(stored === mockEngine)
        #expect(manager.state(for: "TUI") == .running)
    }

    @Test("Process termination marks pane exited")
    @MainActor
    func testProcessTerminationMarksPaneExited() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        let mockSession = MockTerminalSession()

        manager.installSession(mockSession, for: "TUI")
        manager.engineManager?.isEngineReady = true
        manager.processTerminated(name: "TUI", exitCode: 0)

        #expect(manager.state(for: "TUI") == .exited(exitCode: 0))
        #expect(manager.engine(for: "TUI") == nil)
        #expect(manager.engineManager?.isEngineReady == true)
        #expect(mockSession.terminateCalls == 0)
    }

    @Test("Shutdown terminates running pane processes")
    @MainActor
    func testShutdownTerminatesRunningPaneProcesses() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)
        let runningSession = MockTerminalSession()
        let exitedSession = MockTerminalSession()

        manager.installSession(runningSession, for: "TUI")
        manager.installSession(exitedSession, for: "Agent Alpha", state: .exited(exitCode: 0))
        manager.engineManager?.isEngineReady = true

        manager.shutdown()

        #expect(runningSession.terminateCalls == 1)
        #expect(exitedSession.terminateCalls == 0)
        #expect(manager.engineManager?.isEngineReady == false)
    }

    @Test("Nested State Propagation")
    @MainActor
    func testNestedStatePropagation() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)

        var didFire = false

        let cancellable = manager.objectWillChange.sink { _ in
            didFire = true
        }

        manager.engineManager?.isEngineReady = true

        // Yield to allow Combine to process the event
        try await Task.sleep(nanoseconds: 10_000_000)

        #expect(didFire)
        cancellable.cancel()
    }

    @Test("Shutdown clears ready state")
    @MainActor
    func testShutdownClearsEngineReadiness() async throws {
        let monitor = ActivityMonitor()
        let manager = TerminalManager(monitor: monitor)

        manager.engineManager?.isEngineReady = true
        manager.shutdown()

        #expect(manager.engineManager?.isEngineReady == false)
    }

    @Test("Launch path consumes typed terminal settings from the native store")
    @MainActor
    func testMakeSessionUsesSettingsStoreSnapshot() async throws {
        let defaults = try #require(UserDefaults(suiteName: "OrbitCockpitTests.TerminalManager.\(UUID().uuidString)"))
        var configured = OrbitCockpitSettings.defaults
        configured.terminal.fontSize = 15
        configured.terminal.usesNerdFont = false
        configured.terminal.useBrightColorsForBoldText = false
        configured.terminal.useCustomBlockGlyphs = false
        configured.terminal.antiAliasCustomBlockGlyphs = true
        configured.appearance.terminalColorSchemePreference = .dark
        configured.linksAndInput.optionKeySendsMeta = false
        configured.linksAndInput.mouseReportingEnabled = false
        configured.linksAndInput.backspaceSendsControlH = true
        configured.advanced.scrollbackLineLimit = 20_000
        configured.advanced.cursorStyle = .steadyBar
        configured.advanced.termName = "xterm-gh-orbit"
        configured.advanced.tabWidth = 4
        configured.advanced.screenReaderMode = true
        configured.advanced.sixelSupportEnabled = false
        configured.advanced.ansi256PaletteStrategy = .xterm
        defaults.set(try JSONEncoder().encode(configured), forKey: OrbitCockpitSettingsStore.defaultStorageKey)
        let reloadedStore = OrbitCockpitSettingsStore(defaults: defaults)

        let launcher = RecordingSessionLauncher()
        let manager = TerminalManager(
            monitor: ActivityMonitor(),
            settingsStore: reloadedStore,
            sessionLauncher: launcher)
        let request = TerminalLaunchRequest(
            executable: URL(fileURLWithPath: "/usr/bin/env"),
            arguments: ["true"],
            environment: nil,
            currentDirectoryURL: nil)

        _ = manager.makeSession(request: request, onTerminate: { _ in })

        #expect(launcher.lastRequest == request)
        #expect(
            launcher.lastSettings
                == TerminalSessionSettings(
                    fontSize: 15,
                    usesNerdFont: false,
                    useBrightColorsForBoldText: false,
                    useCustomBlockGlyphs: false,
                    antiAliasCustomBlockGlyphs: true,
                    colorSchemePreference: .dark,
                    optionKeySendsMeta: false,
                    mouseReportingEnabled: false,
                    backspaceSendsControlH: true))
        #expect(
            launcher.lastStartupSettings
                == TerminalStartupSettings(
                    scrollbackLineLimit: 20_000,
                    cursorStyle: .steadyBar,
                    termName: "xterm-gh-orbit",
                    tabWidth: 4,
                    screenReaderMode: true,
                    sixelSupportEnabled: false,
                    ansi256PaletteStrategy: .xterm))
    }

    @Test("Running sessions receive live-applied terminal settings updates")
    @MainActor
    func testSettingsStoreUpdatesRunningSessions() async throws {
        let defaults = try #require(UserDefaults(suiteName: "OrbitCockpitTests.LiveApply.\(UUID().uuidString)"))
        let store = OrbitCockpitSettingsStore(defaults: defaults)
        let manager = TerminalManager(monitor: ActivityMonitor(), settingsStore: store)
        let engine = MockTerminalEngine()
        let session = MockTerminalSession(engine: engine)

        manager.installSession(session, for: "TUI")
        #expect(engine.appliedSettings.count == 1)

        store.binding(\.terminal.fontSize).wrappedValue = 18
        store.binding(\.linksAndInput.optionKeySendsMeta).wrappedValue = false
        store.binding(\.linksAndInput.backspaceSendsControlH).wrappedValue = true

        for _ in 0..<20 {
            if let latest = engine.appliedSettings.last,
                latest.0.fontSize == 18,
                latest.0.optionKeySendsMeta == false,
                latest.0.backspaceSendsControlH
            {
                break
            }
            try await Task.sleep(nanoseconds: 10_000_000)
        }

        let latest = try #require(engine.appliedSettings.last)
        #expect(latest.0.fontSize == 18)
        #expect(!latest.0.optionKeySendsMeta)
        #expect(latest.0.backspaceSendsControlH)
    }

    @Test("Startup-only settings do not live-apply to running sessions")
    @MainActor
    func testStartupSettingsDoNotUpdateRunningSessions() async throws {
        let defaults = try #require(UserDefaults(suiteName: "OrbitCockpitTests.StartupOnly.\(UUID().uuidString)"))
        let store = OrbitCockpitSettingsStore(defaults: defaults)
        let manager = TerminalManager(monitor: ActivityMonitor(), settingsStore: store)
        let engine = MockTerminalEngine()
        let session = MockTerminalSession(engine: engine)

        manager.installSession(session, for: "TUI")
        #expect(engine.appliedSettings.count == 1)

        store.binding(\.advanced.cursorStyle).wrappedValue = .steadyBar
        store.binding(\.advanced.scrollbackLineLimit).wrappedValue = 20_000
        store.binding(\.advanced.termName).wrappedValue = "xterm-gh-orbit"

        try await Task.sleep(nanoseconds: 20_000_000)

        #expect(engine.appliedSettings.count == 1)
    }

    @Test("New sessions inherit updated settings snapshots")
    @MainActor
    func testNewSessionsInheritUpdatedSettings() async throws {
        let defaults = try #require(UserDefaults(suiteName: "OrbitCockpitTests.NewSessions.\(UUID().uuidString)"))
        let store = OrbitCockpitSettingsStore(defaults: defaults)
        let launcher = RecordingSessionLauncher()
        let manager = TerminalManager(
            monitor: ActivityMonitor(),
            settingsStore: store,
            sessionLauncher: launcher)

        store.binding(\.terminal.fontSize).wrappedValue = 17
        store.binding(\.linksAndInput.mouseReportingEnabled).wrappedValue = false
        store.binding(\.advanced.cursorStyle).wrappedValue = .steadyUnderline
        store.binding(\.advanced.tabWidth).wrappedValue = 4
        store.binding(\.advanced.sixelSupportEnabled).wrappedValue = false

        _ = manager.makeSession(
            request: TerminalLaunchRequest(
                executable: URL(fileURLWithPath: "/usr/bin/env"),
                arguments: ["true"],
                environment: nil,
                currentDirectoryURL: nil),
            onTerminate: { _ in })

        let launchedSettings = try #require(launcher.lastSettings)
        #expect(launchedSettings.fontSize == 17)
        #expect(!launchedSettings.mouseReportingEnabled)
        let launchedStartupSettings = try #require(launcher.lastStartupSettings)
        #expect(launchedStartupSettings.cursorStyle == .steadyUnderline)
        #expect(launchedStartupSettings.tabWidth == 4)
        #expect(!launchedStartupSettings.sixelSupportEnabled)
    }
}
