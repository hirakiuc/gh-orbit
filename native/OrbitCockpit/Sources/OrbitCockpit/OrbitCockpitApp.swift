import AppKit
import Combine
import SwiftUI

@main
@MainActor
struct OrbitCockpitApp: App {
    @StateObject private var activityMonitor: ActivityMonitor
    @StateObject private var terminalManager: TerminalManager

    init() {
        // App Lifecycle Logging (Safe: No environment variables exposed)
        let monitor = ActivityMonitor()
        monitor.log(component: "[App]", level: .info, message: "Launched Orbit Cockpit")
        _activityMonitor = StateObject(wrappedValue: monitor)
        _terminalManager = StateObject(wrappedValue: TerminalManager(monitor: monitor))
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(activityMonitor)
                .environmentObject(terminalManager)
                .onReceive(NotificationCenter.default.publisher(for: NSApplication.willTerminateNotification)) { _ in
                    terminalManager.shutdown()
                }
        }
    }
}

@MainActor
struct ContentView: View {
    @State private var selectedPane: String? = "TUI"
    @State private var showDebugLogs: Bool = false
    @Environment(\.colorScheme) var colorScheme
    @EnvironmentObject var activityMonitor: ActivityMonitor
    @EnvironmentObject var terminalManager: TerminalManager

    var body: some View {
        NavigationSplitView {
            Sidebar(selectedPane: $selectedPane)
                .environmentObject(terminalManager)
        } detail: {
            VStack(spacing: 0) {
                if let selectedPane = selectedPane {
                    TerminalHostView(paneName: selectedPane)
                        .environmentObject(terminalManager)
                } else {
                    Text("Select a pane")
                        .foregroundColor(.secondary)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }

                if showDebugLogs {
                    Divider()
                    LogConsoleView(logs: activityMonitor.logs)
                        .frame(height: 200)
                }
            }
        }
        .toolbar {
            ToolbarItem {
                Button(action: { showDebugLogs.toggle() }) {
                    Label("Debug Logs", systemImage: "ladybug")
                        .foregroundColor(showDebugLogs ? .accentColor : .secondary)
                }
                .help("Toggle Activity Monitor")
            }
        }
        .onChange(of: colorScheme) { _, newValue in
            terminalManager.updateTheme(isDark: newValue == .dark)
        }
        .onAppear {
            terminalManager.updateTheme(isDark: colorScheme == .dark)
        }
    }
}

@MainActor
struct Sidebar: View {
    @Binding var selectedPane: String?
    @EnvironmentObject var terminalManager: TerminalManager

    var body: some View {
        List(selection: $selectedPane) {
            Section("Triage") {
                HStack {
                    Label("TUI", systemImage: "terminal")
                    Spacer()
                    Circle()
                        .fill(terminalManager.engineManager?.isEngineReady == true ? Color.green : Color.yellow)
                        .frame(width: 8, height: 8)
                }
                .tag("TUI")
            }

            Section("AI Agents") {
                Label("Agent Alpha", systemImage: "bolt.fill")
                    .tag("Agent Alpha")
                Label("Agent Beta", systemImage: "wand.and.stars")
                    .tag("Agent Beta")
            }
        }
        .navigationTitle("Orbit Cockpit")
    }
}

@MainActor
struct TerminalHostView: View {
    let paneName: String
    @EnvironmentObject var terminalManager: TerminalManager

    var body: some View {
        VStack {
            switch terminalManager.state(for: paneName) {
            case .failed(let error):
                VStack(spacing: 20) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.largeTitle)
                        .foregroundColor(.yellow)
                    Text(error)
                        .font(.headline)
                        .multilineTextAlignment(.center)
                    Button("Retry") {
                        terminalManager.launch(paneName)
                    }
                    .buttonStyle(.borderedProminent)
                }
                .padding()
            case .running:
                if let engine = terminalManager.engine(for: paneName) {
                    TerminalContainer(engine: engine, isFocused: true)
                } else {
                    ProgressView()
                }
            case .exited(let exitCode):
                VStack(spacing: 20) {
                    Image(systemName: "terminal")
                        .font(.largeTitle)
                        .foregroundColor(.secondary)
                    Text("Session ended with exit code \(exitCode)")
                        .font(.headline)
                        .foregroundColor(.secondary)
                    Button("Restart TUI") {
                        terminalManager.launch(paneName)
                    }
                    .buttonStyle(.borderedProminent)
                }
                .padding()
            case .launching, .none:
                VStack(spacing: 12) {
                    ProgressView()
                    Text("Initializing Engine...")
                        .foregroundColor(.secondary)
                }
                .onAppear {
                    terminalManager.launch(paneName)
                }
            }
        }
        .navigationTitle(paneName)
    }
}

@MainActor
enum TerminalPaneState: Equatable {
    case launching
    case running
    case exited(exitCode: Int32)
    case failed(message: String)
}

@MainActor
protocol TerminalProcessSession: AnyObject {
    var engine: OrbitTerminalEngine { get }
    func terminateProcess()
}

extension SwiftTermAdapter: TerminalProcessSession {
    var engine: OrbitTerminalEngine {
        self
    }
}

@MainActor
struct TerminalPaneSession {
    var state: TerminalPaneState
    var session: TerminalProcessSession?
}

@MainActor
class TerminalManager: ObservableObject {
    @Published private var panes: [String: TerminalPaneSession] = [:]
    @Published var engineManager: NativeEngineManager?

    private var isDark: Bool = true
    private var cancellables = Set<AnyCancellable>()
    private var onLog: ((String, LogLevel) -> Void)?
    private var launchTasks: [String: Task<Void, Never>] = [:]
    private let runtimeConfiguration: EngineRuntimeConfiguration

    init(monitor: ActivityMonitor, runtimeConfiguration: EngineRuntimeConfiguration = EngineRuntimeConfiguration()) {
        self.runtimeConfiguration = runtimeConfiguration
        let logFunc: (String, LogLevel) -> Void = { msg, level in
            monitor.log(component: "[App]", level: level, message: msg)
        }
        self.onLog = logFunc

        let newManager = NativeEngineManager(
            runtimeConfiguration: runtimeConfiguration,
            onLog: { msg, level in
                monitor.log(component: "[Engine]", level: level, message: msg)
            })

        newManager.objectWillChange
            .sink { [weak self] _ in
                self?.objectWillChange.send()
            }
            .store(in: &cancellables)

        self.engineManager = newManager
    }

    func updateTheme(isDark: Bool) {
        self.isDark = isDark
        for pane in panes.values {
            pane.session?.engine.isDarkMode(isDark)
        }
    }

    func state(for name: String) -> TerminalPaneState? {
        panes[name]?.state
    }

    func engine(for name: String) -> OrbitTerminalEngine? {
        panes[name]?.session?.engine
    }

    func installSession(_ session: TerminalProcessSession, for name: String, state: TerminalPaneState = .running) {
        panes[name] = TerminalPaneSession(state: state, session: session)
    }

    func launch(_ name: String) {
        if launchTasks[name] != nil {
            return
        }

        if let state = panes[name]?.state, state == .launching || state == .running {
            return
        }

        panes[name] = TerminalPaneSession(state: .launching, session: nil)

        let task = Task { @MainActor in
            defer { launchTasks[name] = nil }
            let adapter = SwiftTermAdapter(
                onLog: onLog,
                onTerminate: { [weak self] exitCode in
                    self?.processTerminated(name: name, exitCode: exitCode)
                })
            adapter.isDarkMode(isDark)

            onLog?("Resolving gh-orbit binary...", .debug)
            // 1. Resolve binary
            guard let executableURL = PathResolver.resolveBinary(onLog: onLog) else {
                onLog?("gh-orbit binary not found. Launch aborted.", .error)
                panes[name] = TerminalPaneSession(
                    state: .failed(
                        message: "gh-orbit binary not found. Please ensure it's in your PATH or set GH_ORBIT_BIN."),
                    session: nil)
                return
            }
            onLog?("Final binary resolved to: \(executableURL.path)", .debug)

            // Propagate environment including GH_TOKEN if available
            var env = runtimeConfiguration.environment
            env["GH_ORBIT_REQUIRE_ENGINE"] = "1"

            // 2. Ensure Engine is running
            if let engineMgr = engineManager {
                onLog?("Delegating to NativeEngineManager to start background engine...", .debug)
                switch await engineMgr.startEngine(executable: executableURL, environment: env) {
                case .reused, .ownedReady:
                    break
                case .failed(let message):
                    onLog?("Engine verification failed. Aborting pane launch.", .error)
                    panes[name] = TerminalPaneSession(state: .failed(message: message), session: nil)
                    return
                }
            }

            // 3. Launch TUI
            var args: [String] = []
            if name != "TUI" {
                args = ["agent", "--name", name]
            }

            onLog?("Launching TUI process with args: \(args)", .debug)
            adapter.startProcess(
                executable: executableURL, args: args, environment: env.map { "\($0.key)=\($0.value)" })
            panes[name] = TerminalPaneSession(state: .running, session: adapter)
        }
        launchTasks[name] = task
    }

    func processTerminated(name: String, exitCode: Int32?) {
        panes[name] = TerminalPaneSession(state: .exited(exitCode: exitCode ?? -1), session: nil)
    }

    func shutdown() {
        for task in launchTasks.values {
            task.cancel()
        }
        launchTasks.removeAll()
        for pane in panes.values where pane.state == .running {
            pane.session?.terminateProcess()
        }
        engineManager?.stopEngine()
    }
}
