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
    @Environment(\.colorScheme) var colorScheme

    // In a real app, these would be managed in a ViewModel/Store
    @StateObject private var terminalManager = TerminalManager()

    var body: some View {
        NavigationSplitView {
            Sidebar(selectedPane: $selectedPane)
        } detail: {
            if let selectedPane = selectedPane {
                TerminalHostView(paneName: selectedPane)
                    .environmentObject(terminalManager)
            } else {
                Text("Select a pane")
                    .foregroundColor(.secondary)
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

    var body: some View {
        List(selection: $selectedPane) {
            Section("Triage") {
                Label("TUI", systemImage: "terminal")
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
            if let engine = terminalManager.engines[paneName] {
                TerminalContainer(engine: engine, isFocused: true)
            } else {
                ProgressView("Launching \(paneName)...")
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
    private var isDark: Bool = true

    func updateTheme(isDark: Bool) {
        self.isDark = isDark
        for engine in engines.values {
            engine.isDarkMode(isDark)
        }
    }

    func launch(_ name: String) {
        let adapter = SwiftTermAdapter()
        adapter.isDarkMode(isDark)

        // Robust binary resolution
        if let executableURL = Bundle.main.url(forAuxiliaryExecutable: "gh-orbit") {
            var args: [String] = []
            if name == "TUI" {
                args = []  // Normal TUI mode
            } else {
                args = ["agent", "--name", name]  // Conceptual agent mode
            }

            adapter.startProcess(executable: executableURL, args: args, environment: nil)
            engines[name] = adapter
        } else {
            // Fallback for dev environment
            let devPath = URL(fileURLWithPath: "bin/gh-orbit")
            adapter.startProcess(executable: devPath, args: [], environment: nil)
            engines[name] = adapter
        }
    }
}
