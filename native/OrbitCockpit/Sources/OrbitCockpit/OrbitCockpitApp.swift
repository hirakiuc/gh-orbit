import AppKit
import Combine
import SwiftUI

@main
@MainActor
struct OrbitCockpitApp: App {
    @StateObject private var activityMonitor: ActivityMonitor
    @StateObject private var terminalManager: TerminalManager
    @StateObject private var reviewRequestStore: ReviewRequestStore
    @StateObject private var reviewWorkspaceManager: ReviewWorkspaceManager
    private let reviewWorkspaceRequestInbox: ReviewWorkspaceRequestInbox

    init() {
        // App Lifecycle Logging (Safe: No environment variables exposed)
        let monitor = ActivityMonitor()
        monitor.log(component: "[App]", level: .info, message: "Launched Orbit Cockpit")
        _activityMonitor = StateObject(wrappedValue: monitor)
        let terminalManager = TerminalManager(monitor: monitor)
        _terminalManager = StateObject(wrappedValue: terminalManager)
        _reviewRequestStore = StateObject(wrappedValue: ReviewRequestStore())
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
                .environmentObject(terminalManager)
                .environmentObject(reviewRequestStore)
                .environmentObject(reviewWorkspaceManager)
                .onReceive(NotificationCenter.default.publisher(for: NSApplication.willTerminateNotification)) { _ in
                    reviewWorkspaceRequestInbox.stop()
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
    @EnvironmentObject var reviewRequestStore: ReviewRequestStore
    @EnvironmentObject var reviewWorkspaceManager: ReviewWorkspaceManager

    var body: some View {
        NavigationSplitView {
            Sidebar(selectedPane: $selectedPane)
                .environmentObject(terminalManager)
                .environmentObject(reviewRequestStore)
                .environmentObject(reviewWorkspaceManager)
        } detail: {
            VStack(spacing: 0) {
                if let selectedPane = selectedPane {
                    if let workspace = reviewWorkspaceManager.workspace(forPaneName: selectedPane) {
                        ReviewWorkspaceHostView(workspace: workspace)
                            .environmentObject(terminalManager)
                    } else if let request = reviewRequestStore.request(forPaneName: selectedPane) {
                        ReviewRequestDetailView(request: request)
                            .environmentObject(reviewWorkspaceManager)
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
struct ReviewRequestDetailView: View {
    let request: NativeReviewRequest
    @EnvironmentObject var reviewWorkspaceManager: ReviewWorkspaceManager

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            VStack(alignment: .leading, spacing: 8) {
                Text(request.title)
                    .font(.title2)
                    .fontWeight(.semibold)
                Text(request.subtitle)
                    .foregroundColor(.secondary)
                Text("\(request.repository.host)/\(request.repository.owner)/\(request.repository.name)")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
                Text("Pull request #\(request.pullRequestNumber)")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
            }

            Button("Start review workspace") {
                _ = reviewWorkspaceManager.startReviewWorkspace(for: request)
            }
            .buttonStyle(.borderedProminent)

            Text("If the pull request head changes, starting again creates a new workspace for the new review target.")
                .font(.footnote)
                .foregroundColor(.secondary)

            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .padding(24)
        .navigationTitle(request.displayName)
    }
}

@MainActor
struct ReviewWorkspaceHostView: View {
    let workspace: ReviewWorkspace
    @EnvironmentObject var terminalManager: TerminalManager
    @EnvironmentObject var reviewWorkspaceManager: ReviewWorkspaceManager

    var body: some View {
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
        case .failed(let message):
            ContentUnavailableView(
                "Review workspace failed", systemImage: "exclamationmark.triangle", description: Text(message))
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

@MainActor
struct Sidebar: View {
    @Binding var selectedPane: String?
    @EnvironmentObject var terminalManager: TerminalManager
    @EnvironmentObject var reviewRequestStore: ReviewRequestStore
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

            Section("Review Requests") {
                ForEach(reviewRequestStore.requests) { request in
                    HStack {
                        Label(request.title, systemImage: "person.crop.rectangle.badge.checkmark")
                        Spacer()
                        Text("PR #\(request.pullRequestNumber)")
                            .foregroundColor(.secondary)
                    }
                    .tag(request.paneName)
                }
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
        case .failed(let message): message
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
        isDark: Bool,
        onLog: ((String, LogLevel) -> Void)?,
        onTerminate: @escaping (Int32?) -> Void
    ) -> TerminalProcessSession
}

@MainActor
struct SwiftTermSessionLauncher: TerminalSessionLaunching {
    func launchSession(
        request: TerminalLaunchRequest,
        isDark: Bool,
        onLog: ((String, LogLevel) -> Void)?,
        onTerminate: @escaping (Int32?) -> Void
    ) -> TerminalProcessSession {
        let adapter = SwiftTermAdapter(onLog: onLog, onTerminate: onTerminate)
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
    let runtimeConfiguration: EngineRuntimeConfiguration

    init(
        monitor: ActivityMonitor,
        runtimeConfiguration: EngineRuntimeConfiguration = EngineRuntimeConfiguration(),
        sessionLauncher: any TerminalSessionLaunching = SwiftTermSessionLauncher()
    ) {
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
            isDark: isDark,
            onLog: onLog,
            onTerminate: onTerminate
        )
    }

    func installSession(_ session: TerminalProcessSession, for name: String, state: TerminalPaneState = .running) {
        guard !name.hasPrefix(Self.workspacePanePrefix) else { return }
        panes[name] = TerminalPaneSession(state: state, session: session)
    }

    func reserveWorkspacePane(_ name: String) -> Bool {
        guard name.hasPrefix(Self.workspacePanePrefix), panes[name] == nil else { return false }
        panes[name] = TerminalPaneSession(state: .launching, session: nil)
        return true
    }

    func installWorkspaceSession(_ session: TerminalProcessSession, for name: String) -> Bool {
        guard name.hasPrefix(Self.workspacePanePrefix), panes[name] != nil else { return false }
        panes[name] = TerminalPaneSession(state: .running, session: session)
        return true
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
