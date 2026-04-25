import SwiftUI

struct LogConsoleView: View {
    let logs: String

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 4) {
                    Text(logs)
                        .font(.system(.caption, design: .monospaced))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(8)
                        .id("bottom")
                }
            }
            .background(Color(NSColor.textBackgroundColor))
            .onChange(of: logs) { _, _ in
                // Auto-scroll to bottom on update
                withAnimation {
                    proxy.scrollTo("bottom", anchor: .bottom)
                }
            }
        }
    }
}
