import Combine
import Foundation
import SwiftUI

public enum LogLevel: Int, Comparable {
    case debug = 0
    case info = 1
    case warning = 2
    case error = 3

    public static func < (lhs: LogLevel, rhs: LogLevel) -> Bool {
        return lhs.rawValue < rhs.rawValue
    }
}

public struct LogEntry: Identifiable, Equatable {
    // swiftlint:disable:next identifier_name
    public let id = UUID()
    public let timestamp: Date
    public let component: String
    public let level: LogLevel
    public let message: String
}

/// ActivityMonitor serves as the centralized log aggregator and publisher for the native app.
/// It debounces high-volume updates to protect SwiftUI rendering performance.
@MainActor
class ActivityMonitor: ObservableObject {
    /// The aggregated log stream for UI display.
    @Published var logs: [LogEntry] = []

    /// Bounded buffer for logs
    private var logBuffer: [LogEntry] = []
    private let maxLogLines = 5000

    // Performance: Debounce UI updates
    private var pendingLogs: Bool = false
    private var logTimer: Timer?

    init() {
        startLogTimer()
    }

    /// Appends a new log entry with the specified component prefix and severity.
    /// - Parameters:
    ///   - component: The source of the log (e.g., "[App]", "[Engine]").
    ///   - level: The severity level.
    ///   - message: The log content.
    func log(component: String, level: LogLevel = .info, message: String) {
        let isFirstLog = logBuffer.isEmpty
        let entry = LogEntry(timestamp: Date(), component: component, level: level, message: message)

        logBuffer.append(entry)
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
        logs = logBuffer
        pendingLogs = false
    }

    /// Synchronously returns the current log buffer content.
    func getLogs() -> [LogEntry] {
        return logBuffer
    }
}
