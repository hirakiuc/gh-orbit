import SwiftUI

@main
@MainActor
struct OrbitCockpitApp: App {
    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}

@MainActor
struct ContentView: View {
    @State private var selectedPane: String? = "TUI"
    @State private var showDebugLogs: Bool = false
    @Environment(\.colorScheme) var colorScheme

    // In a real app, these would be managed in a ViewModel/Store
    @StateObject private var terminalManager = TerminalManager()

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
                    LogConsoleView(logs: terminalManager.engineManager.engineLog)
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
                .help("Toggle Engine Debug Logs")
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
                        .fill(terminalManager.engineManager.isEngineReady ? Color.green : Color.yellow)
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
            if let error = terminalManager.launchError {
                VStack(spacing: 20) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.largeTitle)
                        .foregroundColor(.yellow)
                    Text(error)
                        .font(.headline)
                        .multilineTextAlignment(.center)
                    Button("Retry") {
                        terminalManager.launchError = nil
                        terminalManager.launch(paneName)
                    }
                    .buttonStyle(.borderedProminent)
                }
                .padding()
            } else if let engine = terminalManager.engines[paneName] {
                TerminalContainer(engine: engine, isFocused: true)
            } else {
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
class TerminalManager: ObservableObject {
    @Published var engines: [String: OrbitTerminalEngine] = [:]
    @Published var engineManager = NativeEngineManager()
    @Published var launchError: String?

    private var isDark: Bool = true

    func updateTheme(isDark: Bool) {
        self.isDark = isDark
        for engine in engines.values {
            engine.isDarkMode(isDark)
        }
    }

    func launch(_ name: String) {
        Task {
            let adapter = SwiftTermAdapter()
            adapter.isDarkMode(isDark)

            // 1. Resolve binary
            guard let executableURL = PathResolver.resolveBinary() else {
                self.launchError = "gh-orbit binary not found. Please ensure it's in your PATH or set GH_ORBIT_BIN."
                return
            }

            // 2. Ensure Engine is running
            await engineManager.startEngine(executable: executableURL)

            // 3. Launch TUI
            var args: [String] = []
            if name != "TUI" {
                args = ["agent", "--name", name]
            }

            // Propagate environment including GH_TOKEN if available
            var env = ProcessInfo.processInfo.environment

            // Prioritize App Group container for Sandbox IPC
            let appGroupID = "com.hirakiuc.gh-orbit.cockpit"
            if let groupURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: appGroupID) {
                env["XDG_RUNTIME_DIR"] = groupURL.path
            } else if env["XDG_RUNTIME_DIR"] == nil {
                let home = FileManager.default.homeDirectoryForCurrentUser.path
                env["XDG_RUNTIME_DIR"] = home + "/.local/run"
            }

            adapter.startProcess(
                executable: executableURL, args: args, environment: env.map { "\($0.key)=\($0.value)" })
            engines[name] = adapter
        }
    }
}
