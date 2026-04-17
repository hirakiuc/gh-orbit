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
        terminalView.process.setWinSize(rows: Int32(rows), cols: Int32(cols))
    }
    
    func getBuffer() -> String {
        // SwiftTerm stores content in its buffer. 
        // For the initial implementation, we iterate through the active lines.
        var fullText = ""
        let terminal = terminalView.getTerminal()
        for i in 0..<terminal.rows {
            if let line = terminal.getLine(row: i) {
                fullText += line.toString() + "\n"
            }
        }
        return fullText
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
        // Propagate size changes directly to the underlying process
        source.process.setWinSize(rows: Int32(newRows), cols: Int32(newCols))
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
