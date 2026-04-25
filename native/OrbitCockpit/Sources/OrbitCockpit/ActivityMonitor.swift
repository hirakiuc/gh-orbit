import Combine
import Foundation

/// ActivityMonitor serves as the centralized log aggregator and publisher for the native app.
/// It debounces high-volume updates to protect SwiftUI rendering performance.
@MainActor
class ActivityMonitor: ObservableObject {
    /// The aggregated log stream for UI display.
    @Published var fullLog: String = ""

    /// Bounded buffer for logs (approx 5MB)
    private var logBuffer: [String] = []
    private let maxLogLines = 5000

    // Performance: Debounce UI updates
    private var pendingLogs: Bool = false
    private var logTimer: Timer?

    init() {
        startLogTimer()
    }

    // Timer is managed by the actor's lifecycle.
    // In SwiftUI, an @StateObject's lifecycle is tied to the view.

    /// Appends a new log line with the specified component prefix.
    /// - Parameters:
    ///   - component: The source of the log (e.g., "[App]", "[Engine]").
    ///   - message: The log content.
    func log(component: String, message: String) {
        let isFirstLog = logBuffer.isEmpty
        let formattedLine = "\(component) \(message)"

        logBuffer.append(formattedLine)
        if logBuffer.count > maxLogLines {
            logBuffer.removeFirst()
        }

        if isFirstLog {
            publishLogs()
        } else {
            pendingLogs = true
        }
    }

    private func startLogTimer() {
        logTimer = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                if self?.pendingLogs == true {
                    self?.publishLogs()
                }
            }
        }
    }

    private func publishLogs() {
        fullLog = logBuffer.joined(separator: "\n")
        pendingLogs = false
    }

    /// Synchronously returns the current log buffer content.
    func getLogs() -> String {
        return logBuffer.joined(separator: "\n")
    }
}
