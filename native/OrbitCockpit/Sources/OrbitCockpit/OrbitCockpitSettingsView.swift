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
                    Toggle(
                        "Bright colors for bold text",
                        isOn: settingsStore.binding(\.terminal.useBrightColorsForBoldText))
                    Toggle("Custom block glyphs", isOn: settingsStore.binding(\.terminal.useCustomBlockGlyphs))
                    Toggle(
                        "Anti-aliased custom block glyphs",
                        isOn: settingsStore.binding(\.terminal.antiAliasCustomBlockGlyphs))
                } header: {
                    Text("Terminal Defaults")
                } footer: {
                    Text(
                        "These settings apply to running SwiftTerm panes immediately and are also used for new terminal sessions."
                    )
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
                    Toggle("Enable mouse reporting", isOn: settingsStore.binding(\.linksAndInput.mouseReportingEnabled))
                    Toggle(
                        "Backspace sends Control-H", isOn: settingsStore.binding(\.linksAndInput.backspaceSendsControlH)
                    )
                } header: {
                    Text("Links & Input")
                } footer: {
                    Text(
                        "Only the controls SwiftTerm can update safely on existing panes are exposed here as live-applied settings."
                    )
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
                } header: {
                    Text("Rendering")
                }

                Section {

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

                    Picker("Cursor Style", selection: settingsStore.binding(\.advanced.cursorStyle)) {
                        ForEach(TerminalCursorStylePreference.allCases, id: \.self) { option in
                            Text(option.label).tag(option)
                        }
                    }

                    HStack {
                        Text("TERM Value")
                        TextField("xterm-256color", text: settingsStore.binding(\.advanced.termName))
                            .textFieldStyle(.roundedBorder)
                    }

                    Stepper(value: settingsStore.binding(\.advanced.tabWidth), in: 2...16, step: 1) {
                        HStack {
                            Text("Tab Width")
                            Spacer()
                            Text("\(settingsStore.settings.advanced.tabWidth)")
                                .foregroundColor(.secondary)
                        }
                    }

                    Toggle(
                        "Enable screen reader mode", isOn: settingsStore.binding(\.advanced.screenReaderMode))
                    Toggle(
                        "Advertise Sixel support", isOn: settingsStore.binding(\.advanced.sixelSupportEnabled))

                    Picker(
                        "ANSI 256 Palette",
                        selection: settingsStore.binding(\.advanced.ansi256PaletteStrategy)
                    ) {
                        ForEach(TerminalAnsi256PaletteStrategyPreference.allCases, id: \.self) { option in
                            Text(option.label).tag(option)
                        }
                    }
                } header: {
                    Text("New Session Startup")
                } footer: {
                    Text(
                        "These settings apply to new terminal sessions only. Existing panes keep their current startup configuration."
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
