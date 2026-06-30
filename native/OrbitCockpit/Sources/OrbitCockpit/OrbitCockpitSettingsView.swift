import SwiftUI

@MainActor
struct OrbitCockpitSettingsView: View {
    @EnvironmentObject private var settingsStore: OrbitCockpitSettingsStore

    var body: some View {
        TabView {
            Form {
                Section {
                    Stepper(value: settingsStore.binding(\.terminal.fontSize), in: 10...24, step: 1) {
                        HStack {
                            Text("Font Size")
                            Spacer()
                            Text("\(Int(settingsStore.settings.terminal.fontSize)) pt")
                                .foregroundColor(.secondary)
                        }
                    }

                    Toggle("Prefer Nerd Font", isOn: settingsStore.binding(\.terminal.usesNerdFont))
                } header: {
                    Text("Terminal Defaults")
                } footer: {
                    Text("These defaults are consumed when Orbit Cockpit launches new terminal sessions.")
                }
            }
            .formStyle(.grouped)
            .padding(20)
            .tabItem {
                Label("Terminal", systemImage: "terminal")
            }

            Form {
                Section {
                    Picker(
                        "Terminal Theme",
                        selection: settingsStore.binding(\.appearance.terminalColorSchemePreference)
                    ) {
                        ForEach(TerminalColorSchemePreference.allCases, id: \.self) { option in
                            Text(option.label).tag(option)
                        }
                    }

                    Toggle(
                        "Show debug logs on launch", isOn: settingsStore.binding(\.appearance.showDebugLogsByDefault))
                } header: {
                    Text("Appearance")
                } footer: {
                    Text(
                        "Appearance defaults are stored now so later tasks can wire immediate runtime application cleanly."
                    )
                }
            }
            .formStyle(.grouped)
            .padding(20)
            .tabItem {
                Label("Appearance", systemImage: "paintpalette")
            }

            Form {
                Section {
                    Toggle(
                        "Open detected links directly", isOn: settingsStore.binding(\.linksAndInput.openLinksDirectly))
                    Toggle("Treat Option as Meta", isOn: settingsStore.binding(\.linksAndInput.optionKeySendsMeta))
                } header: {
                    Text("Links & Input")
                } footer: {
                    Text("This section establishes typed native ownership for later SwiftTerm input behavior.")
                }
            }
            .formStyle(.grouped)
            .padding(20)
            .tabItem {
                Label("Links & Input", systemImage: "link")
            }

            Form {
                Section {
                    Toggle(
                        "Prefer GPU rendering when available", isOn: settingsStore.binding(\.advanced.preferGPURenderer)
                    )

                    Stepper(
                        value: settingsStore.binding(\.advanced.scrollbackLineLimit), in: 1_000...50_000, step: 1_000
                    ) {
                        HStack {
                            Text("Scrollback Line Limit")
                            Spacer()
                            Text("\(settingsStore.settings.advanced.scrollbackLineLimit)")
                                .foregroundColor(.secondary)
                        }
                    }
                } header: {
                    Text("Advanced")
                } footer: {
                    Text(
                        "Advanced values are persisted behind the native settings model so future engine-specific work stays decoupled from raw storage."
                    )
                }
            }
            .formStyle(.grouped)
            .padding(20)
            .tabItem {
                Label("Advanced", systemImage: "gearshape.2")
            }
        }
        .frame(minWidth: 560, minHeight: 360)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("Reset to Defaults") {
                    settingsStore.resetToDefaults()
                }
            }
        }
    }
}
