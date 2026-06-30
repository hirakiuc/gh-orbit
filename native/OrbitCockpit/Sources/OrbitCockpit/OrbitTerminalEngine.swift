import AppKit
import Foundation

/// OrbitTerminalEngine defines the interface for terminal rendering engines.
/// This allows the Orbit Cockpit to swap between SwiftTerm, libghostty, or other engines.
@MainActor
protocol OrbitTerminalEngine: AnyObject {
    /// The actual view to be embedded in the SwiftUI hierarchy.
    var view: NSView { get }

    /// Feeds raw ANSI data into the terminal for rendering.
    func feed(data: Data)

    /// Sends user input/keys to the underlying process.
    func send(string: String)

    /// Handles terminal dimension changes.
    func resize(cols: Int, rows: Int)

    /// Extracts the current plain-text content of the terminal buffer.
    func getBuffer() -> String

    /// Applies the live-supported terminal settings to an existing terminal view.
    func applyTerminalSettings(_ settings: TerminalSessionSettings, isDark: Bool)

    /// Sets the terminal theme (e.g., Light or Dark).
    func isDarkMode(_ isDark: Bool)
}
