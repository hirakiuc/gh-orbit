import Foundation
import SwiftTerm
import AppKit

class SwiftTermAdapter: NSObject, OrbitTerminalEngine, LocalProcessTerminalViewDelegate {
    private let terminalView: LocalProcessTerminalView
    
    var view: NSView {
        return terminalView
    }
    
    init(frame: CGRect = .zero) {
        self.terminalView = LocalProcessTerminalView(frame: frame)
        super.init()
        self.terminalView.processDelegate = self
    }
    
    func feed(data: Data) {
        terminalView.feed(data: data)
    }
    
    func send(string: String) {
        terminalView.send(string)
    }
    
    func resize(cols: Int, rows: Int) {
        // SwiftTerm handles internal resizing, but we might need to notify the pty
    }
    
    func getBuffer() -> String {
        // SwiftTerm doesn't have a direct "get all text" method easily exposed
        // usually you'd iterate through the lines. For MVP, we return a placeholder
        // or a basic implementation if available.
        return ""
    }
    
    func isDarkMode(_ isDark: Bool) {
        if isDark {
            terminalView.nativeBackgroundColor = .black
            terminalView.nativeForegroundColor = .white
        } else {
            terminalView.nativeBackgroundColor = .white
            terminalView.nativeForegroundColor = .black
        }
    }
    
    // MARK: - LocalProcessTerminalViewDelegate
    
    func sizeChanged(source: LocalProcessTerminalView, newCols: Int, newRows: Int) {
        // Notify the parent if necessary
    }
    
    func setTerminalTitle(source: LocalProcessTerminalView, title: String) {
        // Update window title
    }
    
    func processTerminated(source: LocalProcessTerminalView, exitCode: Int32?) {
        // Handle process exit
    }
    
    /// Launches the gh-orbit helper process.
    func startProcess(executable: URL, args: [String], environment: [String]?) {
        terminalView.startProcess(
            executable: executable.path,
            args: args,
            environment: environment,
            execName: nil
        )
    }
}
