import AppKit
import Foundation
import SwiftTerm

@MainActor
class SwiftTermAdapter: NSObject, OrbitTerminalEngine, @preconcurrency LocalProcessTerminalViewDelegate {
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
        guard terminalView.process.running else { return }
        var size = winsize(ws_row: UInt16(rows), ws_col: UInt16(cols), ws_xpixel: 0, ws_ypixel: 0)
        _ = PseudoTerminalHelpers.setWinSize(masterPtyDescriptor: terminalView.process.childfd, windowSize: &size)
    }

    func getBuffer() -> String {
        var fullText = ""
        let terminal = terminalView.getTerminal()
        for i in 0..<terminal.rows {
            if let line = terminal.getLine(row: i) {
                for j in 0..<line.count {
                    let charData = line[j]
                    fullText.append(charData.getCharacter())
                }
                fullText.append("\n")
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
        guard source.process.running else { return }
        var size = winsize(ws_row: UInt16(newRows), ws_col: UInt16(newCols), ws_xpixel: 0, ws_ypixel: 0)
        _ = PseudoTerminalHelpers.setWinSize(masterPtyDescriptor: source.process.childfd, windowSize: &size)
    }

    func setTerminalTitle(source: LocalProcessTerminalView, title: String) {
        // Implementation
    }

    func processTerminated(source: TerminalView, exitCode: Int32?) {
        // Implementation
    }

    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {
        // Implementation
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
