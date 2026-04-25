import AppKit
import SwiftUI

struct LogConsoleView: View {
    let logs: String

    var body: some View {
        ZStack(alignment: .topTrailing) {
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: 4) {
                        if logs.isEmpty {
                            Text("Initializing log stream...")
                                .foregroundColor(.secondary)
                                .italic()
                        } else {
                            Text(logs)
                                .font(.system(.caption, design: .monospaced))
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        Color.clear.frame(height: 1).id("bottom")
                    }
                    .padding(8)
                }
                .background(Color(NSColor.textBackgroundColor))
                .onChange(of: logs) { _, _ in
                    // Auto-scroll to bottom on update
                    proxy.scrollTo("bottom", anchor: .bottom)
                }
            }

            // Overlay Controls
            HStack {
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
        pasteboard.setString(logs, forType: .string)
    }
}
