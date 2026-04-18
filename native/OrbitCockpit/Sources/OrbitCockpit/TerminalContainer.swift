import AppKit
import SwiftUI

@MainActor
struct TerminalContainer: NSViewRepresentable {
    let engine: OrbitTerminalEngine
    let isFocused: Bool

    func makeNSView(context: Context) -> NSView {
        let container = ThrottledContainerView(engine: engine)
        return container
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        if isFocused {
            DispatchQueue.main.async {
                nsView.window?.makeFirstResponder(engine.view)
            }
        }
    }
}

/// A container view that detects visibility and occlusions to pause terminal rendering.
@MainActor
class ThrottledContainerView: NSView {
    private let engine: OrbitTerminalEngine

    init(engine: OrbitTerminalEngine) {
        self.engine = engine
        super.init(frame: .zero)

        self.addSubview(engine.view)
        engine.view.autoresizingMask = [.width, .height]
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) not implemented")
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()

        if let window = window {
            // View entered hierarchy
            setupOcclusionObserver(for: window)
            resumeRendering()
        } else {
            // View removed from hierarchy
            NotificationCenter.default.removeObserver(
                self, name: NSWindow.didChangeOcclusionStateNotification, object: nil)
            pauseRendering()
        }
    }

    private func setupOcclusionObserver(for window: NSWindow) {
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(handleOcclusion),
            name: NSWindow.didChangeOcclusionStateNotification,
            object: window
        )
    }

    @objc private func handleOcclusion() {
        guard let window = window else { return }

        if window.occlusionState.contains(.visible) {
            resumeRendering()
        } else {
            pauseRendering()
        }
    }

    private func pauseRendering() {
        engine.view.isHidden = true
    }

    private func resumeRendering() {
        engine.view.isHidden = false
    }
}
