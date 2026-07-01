import AppKit
import Combine
import SwiftUI

@main
@MainActor
struct OrbitCockpitApp: App {
    @StateObject private var activityMonitor: ActivityMonitor
    @StateObject private var settingsStore: OrbitCockpitSettingsStore
    @StateObject private var terminalManager: TerminalManager
    @StateObject private var reviewWorkspaceManager: ReviewWorkspaceManager
    private let reviewWorkspaceRequestInbox: ReviewWorkspaceRequestInbox

    init() {
        // App Lifecycle Logging (Safe: No environment variables exposed)
        let monitor = ActivityMonitor()
        monitor.log(component: "[App]", level: .info, message: "Launched Orbit Cockpit")
        _activityMonitor = StateObject(wrappedValue: monitor)
        let settingsStore = OrbitCockpitSettingsStore()
        _settingsStore = StateObject(wrappedValue: settingsStore)
        let terminalManager = TerminalManager(monitor: monitor, settingsStore: settingsStore)
        _terminalManager = StateObject(wrappedValue: terminalManager)
        let runtimeConfiguration = terminalManager.runtimeConfiguration
        let workspacePaths = ReviewWorkspacePaths(
            root: URL(fileURLWithPath: runtimeConfiguration.reviewWorkspaceRoot, isDirectory: true))
        let lifecycleController = ReviewWorkspaceLifecycleController(
            store: ReviewWorkspaceMetadataStore(paths: workspacePaths),
            service: ReviewWorkspaceGitService(
                git: URL(fileURLWithPath: "/usr/bin/git"),
                paths: workspacePaths))
        let codexLauncher = CodexReviewWorkspaceLauncher(terminalSessionFactory: terminalManager)
        let reviewWorkspaceManager = ReviewWorkspaceManager(
            terminalManager: terminalManager,
            lifecycleController: lifecycleController,
            codexLauncher: codexLauncher)
        _reviewWorkspaceManager = StateObject(
            wrappedValue: reviewWorkspaceManager)
        let requestInbox = ReviewWorkspaceRequestInbox(
            requestDirectoryURL: URL(
                fileURLWithPath: runtimeConfiguration.reviewWorkspaceRequestDirectory, isDirectory: true),
            onLog: { message, level in
                monitor.log(component: "[ReviewWorkspaceBridge]", level: level, message: message)
            },
            onRequest: { request in
                _ = reviewWorkspaceManager.startReviewWorkspace(for: request.nativeReviewRequest)
            })
        requestInbox.start()
        self.reviewWorkspaceRequestInbox = requestInbox
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(activityMonitor)
                .environmentObject(settingsStore)
                .environmentObject(terminalManager)
                .environmentObject(reviewWorkspaceManager)
                .onReceive(NotificationCenter.default.publisher(for: NSApplication.willTerminateNotification)) { _ in
                    reviewWorkspaceRequestInbox.stop()
                    terminalManager.shutdown()
                }
        }

        Settings {
            OrbitCockpitSettingsView()
                .environmentObject(settingsStore)
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
    @EnvironmentObject var reviewWorkspaceManager: ReviewWorkspaceManager

    var body: some View {
        NavigationSplitView {
            Sidebar(selectedPane: $selectedPane)
                .environmentObject(terminalManager)
                .environmentObject(reviewWorkspaceManager)
        } detail: {
            VStack(spacing: 0) {
                if let selectedPane = selectedPane {
                    if let workspace = reviewWorkspaceManager.workspace(forPaneName: selectedPane) {
                        ReviewWorkspaceHostView(workspace: workspace)
                            .environmentObject(terminalManager)
                    } else {
                        TerminalHostView(paneName: selectedPane)
                            .environmentObject(terminalManager)
                    }
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
            reviewWorkspaceManager.onPaneFocusRequested = { paneName in
                selectedPane = paneName
            }
            reviewWorkspaceManager.restoreManagedWorkspacesIfNeeded()
        }
        .onDisappear {
            reviewWorkspaceManager.onPaneFocusRequested = nil
        }
    }
}

@MainActor
struct ReviewWorkspaceHostView: View {
    let workspace: ReviewWorkspace
    @EnvironmentObject var terminalManager: TerminalManager
    @EnvironmentObject var reviewWorkspaceManager: ReviewWorkspaceManager

    var body: some View {
        VStack(spacing: 0) {
            workspaceContent

            Divider()

            WorkspaceDiagnosticsView(diagnostics: workspace.diagnostics)
                .frame(minHeight: 180, idealHeight: 220, maxHeight: 260)
        }
    }

    @ViewBuilder
    private var workspaceContent: some View {
        switch workspace.state {
        case .available:
            if let path = workspace.record?.worktreePath.path {
                Text("Managed worktree is available at \(path).").foregroundColor(.secondary)
            } else {
                Text("Managed worktree is available.").foregroundColor(.secondary)
            }
        case .running:
            terminalView(or: "Terminal session is unavailable.")
        case .exited(let code):
            if terminalManager.engine(for: workspace.paneName) != nil {
                terminalView(or: "Terminal session exited (\(code)).")
            } else {
                Text("Terminal session exited (\(code)).").foregroundColor(.secondary)
            }
        case .cleanupRequired(let message):
            ContentUnavailableView(
                "Review workspace requires cleanup",
                systemImage: "externaldrive.badge.exclamationmark",
                description: Text(message))
        case .missing(let message):
            ContentUnavailableView(
                "Review workspace missing",
                systemImage: "externaldrive.badge.questionmark",
                description: Text(message))
        case .preparing:
            ProgressView("Preparing review workspace…")
                .onAppear {
                    reviewWorkspaceManager.launchCodexIfNeeded(for: workspace.id)
                }
        case .terminating:
            ProgressView("Terminating review workspace…")
        case .failed(let failure):
            ReviewWorkspaceFailureView(failure: failure)
        }
    }

    @ViewBuilder
    private func terminalView(or unavailable: String) -> some View {
        if let engine = terminalManager.engine(for: workspace.paneName) {
            TerminalContainer(engine: engine, isFocused: true)
        } else {
            Text(unavailable).foregroundColor(.secondary)
        }
    }
}

struct ReviewWorkspaceFailureView: View {
    let failure: ReviewWorkspaceFailure

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                ContentUnavailableView(
                    failure.title,
                    systemImage: failure.systemImage,
                    description: Text(failure.summary)
                )

                VStack(alignment: .leading, spacing: 10) {
                    Text("What you can do next")
                        .font(.headline)

                    ForEach(Array(failure.recoveryGuidance.enumerated()), id: \.offset) { _, step in
                        Label(step, systemImage: "arrow.right.circle")
                            .foregroundColor(.secondary)
                    }
                }

                VStack(alignment: .leading, spacing: 6) {
                    Text("Original error")
                        .font(.headline)
                    Text(failure.message)
                        .font(.system(.caption, design: .monospaced))
                        .foregroundColor(.secondary)
                        .textSelection(.enabled)
                }
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

struct WorkspaceDiagnosticsView: View {
    let diagnostics: [WorkspaceDiagnosticEntry]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Label("Workspace Diagnostics", systemImage: "stethoscope")
                    .font(.headline)
                Spacer()
                Text("\(diagnostics.count)")
                    .font(.caption.monospacedDigit())
                    .foregroundColor(.secondary)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)

            Divider()

            if diagnostics.isEmpty {
                ContentUnavailableView(
                    "Waiting for workspace diagnostics",
                    systemImage: "text.magnifyingglass",
                    description: Text("Events for the selected review workspace will appear here."))
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 8) {
                        ForEach(diagnostics) { diagnostic in
                            WorkspaceDiagnosticRow(entry: diagnostic)
                        }
                    }
                    .padding(12)
                }
                .background(Color(NSColor.textBackgroundColor))
            }
        }
        .background(Color(NSColor.controlBackgroundColor))
    }
}

struct WorkspaceDiagnosticRow: View {
    let entry: WorkspaceDiagnosticEntry

    private static let timestampFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "HH:mm:ss"
        return formatter
    }()

    private var levelColor: Color {
        switch entry.level {
        case .debug:
            return .gray
        case .info:
            return .primary
        case .warning:
            return .yellow
        case .error:
            return .red
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Text(Self.timestampFormatter.string(from: entry.timestamp))
                .foregroundColor(.secondary)
                .frame(width: 56, alignment: .leading)

            Text(entry.category.rawValue.uppercased())
                .foregroundColor(levelColor)
                .frame(width: 82, alignment: .leading)

            Text(entry.message)
                .foregroundColor(levelColor)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(.caption, design: .monospaced))
    }
}

@MainActor
struct Sidebar: View {
    @Binding var selectedPane: String?
    @EnvironmentObject var terminalManager: TerminalManager
    @EnvironmentObject var reviewWorkspaceManager: ReviewWorkspaceManager

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

            Section("Review Workspaces") {
                ForEach(reviewWorkspaceManager.workspaces) { workspace in
                    HStack {
                        Label(workspace.displayName, systemImage: "doc.text.magnifyingglass")
                        Spacer()
                        Text(workspaceStatus(workspace.state)).foregroundColor(.secondary)
                    }
                    .tag(workspace.paneName)
                }
            }
        }
        .navigationTitle("Orbit Cockpit")
    }

    private func workspaceStatus(_ state: ReviewWorkspace.State) -> String {
        switch state {
        case .preparing: "Preparing"
        case .available: "Available"
        case .running: "Running"
        case .terminating: "Stopping"
        case .exited(let code): "Exited (\(code))"
        case .missing: "Missing"
        case .cleanupRequired: "Cleanup required"
        case .failed(let failure): failure.title
        }
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
    func send(string: String)
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
protocol TerminalSessionLaunching {
    func launchSession(
        request: TerminalLaunchRequest,
        settings: TerminalSessionSettings,
        isDark: Bool,
        onLog: ((String, LogLevel) -> Void)?,
        onTerminate: @escaping (Int32?) -> Void
    ) -> TerminalProcessSession
}

@MainActor
struct SwiftTermSessionLauncher: TerminalSessionLaunching {
    func launchSession(
        request: TerminalLaunchRequest,
        settings: TerminalSessionSettings,
        isDark: Bool,
        onLog: ((String, LogLevel) -> Void)?,
        onTerminate: @escaping (Int32?) -> Void
    ) -> TerminalProcessSession {
        let adapter = SwiftTermAdapter(settings: settings, onLog: onLog, onTerminate: onTerminate)
        adapter.isDarkMode(isDark)
        adapter.startProcess(request: request)
        return adapter
    }
}

@MainActor
class TerminalManager: ObservableObject, TerminalSessionCreating {
    static let workspacePanePrefix = "review-workspace:"
    @Published private var panes: [String: TerminalPaneSession] = [:]
    @Published var engineManager: NativeEngineManager?

    private var isDark: Bool = true
    private var cancellables = Set<AnyCancellable>()
    private var onLog: ((String, LogLevel) -> Void)?
    private var launchTasks: [String: Task<Void, Never>] = [:]
    private let sessionLauncher: any TerminalSessionLaunching
    private let settingsStore: OrbitCockpitSettingsStore
    let runtimeConfiguration: EngineRuntimeConfiguration

    init(
        monitor: ActivityMonitor,
        settingsStore: OrbitCockpitSettingsStore = OrbitCockpitSettingsStore(),
        runtimeConfiguration: EngineRuntimeConfiguration = EngineRuntimeConfiguration(),
        sessionLauncher: any TerminalSessionLaunching = SwiftTermSessionLauncher()
    ) {
        self.settingsStore = settingsStore
        self.runtimeConfiguration = runtimeConfiguration
        self.sessionLauncher = sessionLauncher
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

        settingsStore.$settings
            .sink { [weak self] settings in
                self?.applySettingsToRunningSessions(settings.terminalSessionSettings)
            }
            .store(in: &cancellables)

        self.engineManager = newManager
    }

    func updateTheme(isDark: Bool) {
        self.isDark = isDark
        applySettingsToRunningSessions(settingsStore.terminalSessionSettings)
    }

    func state(for name: String) -> TerminalPaneState? {
        panes[name]?.state
    }

    func engine(for name: String) -> OrbitTerminalEngine? {
        panes[name]?.session?.engine
    }

    var managedSocketPath: String? { engineManager?.managedSocketPath }
    var isDarkModeEnabled: Bool { isDark }

    var managedLaunchEnvironment: [String: String] {
        var environment = runtimeConfiguration.environment
        environment["GH_ORBIT_REQUIRE_ENGINE"] = "1"
        environment["GH_ORBIT_REVIEW_WORKSPACE_REQUEST_DIR"] = runtimeConfiguration.reviewWorkspaceRequestDirectory
        return environment
    }

    func makeSession(
        request: TerminalLaunchRequest,
        onTerminate: @escaping (Int32?) -> Void
    ) -> TerminalProcessSession {
        sessionLauncher.launchSession(
            request: request,
            settings: settingsStore.terminalSessionSettings,
            isDark: isDark,
            onLog: onLog,
            onTerminate: onTerminate
        )
    }

    func installSession(_ session: TerminalProcessSession, for name: String, state: TerminalPaneState = .running) {
        guard !name.hasPrefix(Self.workspacePanePrefix) else { return }
        panes[name] = TerminalPaneSession(state: state, session: session)
        session.engine.applyTerminalSettings(settingsStore.terminalSessionSettings, isDark: isDark)
    }

    func reserveWorkspacePane(_ name: String) -> Bool {
        guard name.hasPrefix(Self.workspacePanePrefix), panes[name] == nil else { return false }
        panes[name] = TerminalPaneSession(state: .launching, session: nil)
        return true
    }

    func installWorkspaceSession(_ session: TerminalProcessSession, for name: String) -> Bool {
        guard name.hasPrefix(Self.workspacePanePrefix), panes[name] != nil else { return false }
        panes[name] = TerminalPaneSession(state: .running, session: session)
        session.engine.applyTerminalSettings(settingsStore.terminalSessionSettings, isDark: isDark)
        return true
    }

    private func applySettingsToRunningSessions(_ settings: TerminalSessionSettings) {
        for pane in panes.values {
            pane.session?.engine.applyTerminalSettings(settings, isDark: isDark)
        }
    }

    func terminateWorkspacePane(_ name: String) { panes[name]?.session?.terminateProcess() }

    func workspaceProcessTerminated(_ name: String, exitCode: Int32?) {
        guard name.hasPrefix(Self.workspacePanePrefix), let session = panes[name]?.session else { return }
        panes[name] = TerminalPaneSession(state: .exited(exitCode: exitCode ?? -1), session: session)
    }

    func releaseWorkspacePane(_ name: String) {
        guard name.hasPrefix(Self.workspacePanePrefix) else { return }
        panes[name] = nil
    }

    func launch(_ name: String) {
        guard !name.hasPrefix(Self.workspacePanePrefix) else { return }
        if launchTasks[name] != nil {
            return
        }

        if let state = panes[name]?.state, state == .launching || state == .running {
            return
        }

        panes[name] = TerminalPaneSession(state: .launching, session: nil)

        let task = Task { @MainActor in
            defer { launchTasks[name] = nil }
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
            let env = managedLaunchEnvironment

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
            let session = makeSession(
                request: TerminalLaunchRequest(
                    executable: executableURL,
                    arguments: args,
                    environment: env,
                    currentDirectoryURL: nil
                ),
                onTerminate: { [weak self] exitCode in
                    self?.processTerminated(name: name, exitCode: exitCode)
                })
            panes[name] = TerminalPaneSession(state: .running, session: session)
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
