import AppKit
import Foundation
import SwiftTerm

@MainActor
class SwiftTermAdapter: NSObject, OrbitTerminalEngine, @preconcurrency LocalProcessTerminalViewDelegate {
    private let terminalView: LocalProcessTerminalView
    private var settings: TerminalSessionSettings
    private let processStarter: ((LocalProcessTerminalView, TerminalLaunchRequest) -> Void)?
    private let onLog: ((String, LogLevel) -> Void)?
    private let onTerminate: ((Int32?) -> Void)?

    var view: NSView {
        return terminalView
    }

    init(
        settings: TerminalSessionSettings = .defaults,
        terminalView: LocalProcessTerminalView = LocalProcessTerminalView(frame: .zero),
        processStarter: ((LocalProcessTerminalView, TerminalLaunchRequest) -> Void)? = nil,
        onLog: ((String, LogLevel) -> Void)? = nil,
        onTerminate: ((Int32?) -> Void)? = nil
    ) {
        self.settings = settings
        self.processStarter = processStarter
        self.onLog = onLog
        self.onTerminate = onTerminate
        self.terminalView = terminalView
        super.init()
        self.terminalView.processDelegate = self
        applyTerminalSettings(settings, isDark: false)
    }

    private func setupFont() {
        let fontSize = settings.fontSize
        guard settings.usesNerdFont else {
            terminalView.font = NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
            return
        }

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
            if let font = NSFont(name: name, size: fontSize) {
                selectedFont = font
                onLog?("Found Nerd Font: \(name)", .debug)
                break
            }
        }

        if let font = selectedFont {
            terminalView.font = font
        } else {
            terminalView.font = NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
            onLog?("No Nerd Font found, falling back to system monospaced font.", .warning)
            print("[SwiftTermAdapter] No Nerd Font found, falling back to system monospaced font.")
        }
    }

    private func applyTheme(isDark: Bool) {
        switch settings.colorSchemePreference {
        case .system:
            isDarkMode(isDark)
        case .light:
            terminalView.nativeBackgroundColor = .white
            terminalView.nativeForegroundColor = .black
        case .dark:
            terminalView.nativeBackgroundColor = .black
            terminalView.nativeForegroundColor = .white
        }
    }

    private func refreshDisplay() {
        terminalView.terminal.updateFullScreen()
        terminalView.setNeedsDisplay(terminalView.bounds)
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

    func applyTerminalSettings(_ settings: TerminalSessionSettings, isDark: Bool) {
        self.settings = settings
        setupFont()
        terminalView.optionAsMetaKey = settings.optionKeySendsMeta
        terminalView.allowMouseReporting = settings.mouseReportingEnabled
        terminalView.backspaceSendsControlH = settings.backspaceSendsControlH
        terminalView.useBrightColors = settings.useBrightColorsForBoldText
        terminalView.customBlockGlyphs = settings.useCustomBlockGlyphs
        terminalView.antiAliasCustomBlockGlyphs = settings.antiAliasCustomBlockGlyphs
        applyTheme(isDark: isDark)
        refreshDisplay()
    }

    func isDarkMode(_ isDark: Bool) {
        applyTheme(isDark: isDark)
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
        onTerminate?(exitCode)
    }

    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {
        // Implementation
    }

    func startProcess(request: TerminalLaunchRequest) {
        if let processStarter {
            processStarter(terminalView, request)
            return
        }
        terminalView.startProcess(
            executable: request.executable.path,
            args: request.arguments,
            environment: request.environment.map { $0.map { "\($0.key)=\($0.value)" } },
            execName: nil,
            currentDirectory: request.currentDirectoryURL?.path
        )
    }

    func terminateProcess() {
        terminalView.terminate()
    }
}
