import Foundation
import SwiftTerm
import AppKit

@MainActor
class SwiftTermAdapter: NSObject, OrbitTerminalEngine, LocalProcessTerminalViewDelegate {
    private let terminalView: LocalProcessTerminalView
    
    var view: NSView {
        return terminalView
    }
    
    override init() {
        self.terminalView = LocalProcessTerminalView(frame: .zero)
        super.init()
        self.terminalView.processDelegate = self
    }
    
    func feed(data: Data) {
        let bytes = [UInt8](data)
        terminalView.feed(byteArray: bytes[...])
    }
    
    func send(string: String) {
        terminalView.send(txt: string)
    }
    
    func resize(cols: Int, rows: Int) {
        // Managed by LocalProcessTerminalView
    }
    
    func getBuffer() -> String {
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
    
    nonisolated func sizeChanged(source: LocalProcessTerminalView, newCols: Int, newRows: Int) {
        // Non-isolated callback from PTY
    }
    
    nonisolated func setTerminalTitle(source: LocalProcessTerminalView, title: String) {
        // Non-isolated callback
    }
    
    nonisolated func processTerminated(source: TerminalView, exitCode: Int32?) {
        // Non-isolated callback
    }

    nonisolated func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {
        // Non-isolated callback
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
