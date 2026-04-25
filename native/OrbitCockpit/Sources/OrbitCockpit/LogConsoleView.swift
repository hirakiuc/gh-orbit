import AppKit
import SwiftUI

struct LogConsoleView: View {
    let logs: [LogEntry]
    @State private var showDebug: Bool = false

    var filteredLogs: [LogEntry] {
        if showDebug { return logs }
        return logs.filter { $0.level >= .info }
    }

    var body: some View {
        ZStack(alignment: .topTrailing) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 4) {
                        if logs.isEmpty {
                            Text("Initializing log stream...")
                                .foregroundColor(.secondary)
                                .italic()
                        } else {
                            ForEach(filteredLogs) { entry in
                                LogEntryRow(entry: entry)
                            }
                        }
                        Color.clear.frame(height: 1).id("bottom")
                    }
                    .padding(8)
                }
                .background(Color(NSColor.textBackgroundColor))
                .onChange(of: filteredLogs) { _, _ in
                    // Auto-scroll to bottom on update
                    proxy.scrollTo("bottom", anchor: .bottom)
                }
            }

            // Overlay Controls
            HStack(spacing: 8) {
                Toggle("Debug", isOn: $showDebug)
                    .toggleStyle(.checkbox)
                    .padding(4)
                    .background(Color.secondary.opacity(0.2))
                    .cornerRadius(4)

                Button(action: copyToClipboard) {
                    Label("Copy", systemImage: "doc.on.doc")
                }
                .buttonStyle(.plain)
                .padding(4)
                .background(Color.secondary.opacity(0.2))
                .cornerRadius(4)
                .help("Copy logs to clipboard")
            }
            .padding(8)
        }
    }

    private func copyToClipboard() {
        let pasteboard = NSPasteboard.general
        pasteboard.clearContents()
        let text = filteredLogs.map { "\($0.component) \($0.message)" }.joined(separator: "\n")
        pasteboard.setString(text, forType: .string)
    }
}

struct LogEntryRow: View {
    let entry: LogEntry

    private var levelColor: Color {
        switch entry.level {
        case .debug: return .gray
        case .info: return .primary
        case .warning: return .yellow
        case .error: return .red
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(entry.component)
                .bold()
            Text(entry.message)
        }
        .font(.system(.caption, design: .monospaced))
        .foregroundColor(levelColor)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
