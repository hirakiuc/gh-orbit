import AppKit
import Foundation
import SwiftTerm

@MainActor
class SwiftTermAdapter: NSObject, OrbitTerminalEngine, @preconcurrency LocalProcessTerminalViewDelegate {
    private let terminalView: LocalProcessTerminalView
    private let onLog: ((String, LogLevel) -> Void)?

    var view: NSView {
        return terminalView
    }

    init(onLog: ((String, LogLevel) -> Void)? = nil) {
        self.onLog = onLog
        self.terminalView = LocalProcessTerminalView(frame: .zero)
        super.init()
        self.terminalView.processDelegate = self
        setupFont()
    }

    private func setupFont() {
        // Preferred "Mono" Nerd Fonts for fixed-width icon rendering.
        let preferredFonts = [
            "MonaspiceNe Nerd Font Mono",
            "MonaspiceAr Nerd Font Mono",
            "MonaspiceKr Nerd Font Mono",
            "MonaspiceRn Nerd Font Mono",
            "MonaspiceXe Nerd Font Mono",
            "SauceCodePro Nerd Font Mono",
            "JetBrainsMono Nerd Font Mono",
            "FiraCode Nerd Font Mono",
            "MesloLGS NF Mono",
        ]

        var selectedFont: NSFont?
        for name in preferredFonts {
            if let font = NSFont(name: name, size: 12) {
                selectedFont = font
                onLog?("Found Nerd Font: \(name)", .debug)
                break
            }
        }

        if let font = selectedFont {
            terminalView.font = font
        } else {
            terminalView.font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
            onLog?("No Nerd Font found, falling back to system monospaced font.", .warning)
            print("[SwiftTermAdapter] No Nerd Font found, falling back to system monospaced font.")
        }
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
        let code = exitCode ?? -1
        let message = "\r\n\r\n[Process terminated with exit code \(code)]\r\n"
        let bytes = [UInt8](message.utf8)
        source.feed(byteArray: bytes[...])
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
