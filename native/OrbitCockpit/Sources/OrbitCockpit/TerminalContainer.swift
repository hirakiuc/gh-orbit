import SwiftUI
import AppKit

struct TerminalContainer: NSViewRepresentable, Equatable {
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
    
    static func == (lhs: TerminalContainer, rhs: TerminalContainer) -> Bool {
        return lhs.engine === rhs.engine && lhs.isFocused == rhs.isFocused
    }
}

/// A container view that detects visibility and occlusions to pause terminal rendering.
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
        // Implementation varies by engine. 
        // SwiftTerm doesn't have a formal pause, but we can set visibility 
        // or potentially stop drawing updates.
        engine.view.isHidden = true
    }
    
    private func resumeRendering() {
        engine.view.isHidden = false
    }
}
