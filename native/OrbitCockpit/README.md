# Orbit Cockpit (macOS)

This is the native macOS shell for `gh-orbit`, built with SwiftUI and SwiftTerm.

## Architecture: Modular Hybrid Host

The Cockpit is designed to be a "host" for terminal-based workflows. It uses a protocol-oriented abstraction (`OrbitTerminalEngine`) to allow for pluggable terminal rendering engines.

### Components

- **Go Engine**: The headless core (bundled in `Contents/Helpers`).
- **SwiftUI Wrapper**: The native navigation, notifications, and windowing logic.
- **SwiftTerm**: The initial high-fidelity terminal emulator engine.

## Development

### Prerequisites

- macOS 13.0+
- Xcode 15.0+ or Swift 5.9+ toolchain.
- Go 1.22+ (to build the helper).

### Building via root Makefile

From the project root:

```bash
make cockpit
```

This will:

1. Build the Go engine.
2. Build the Swift app.
3. Bundle them into `bin/OrbitCockpit.app`.
4. Apply ad-hoc signatures.

### Opening in Xcode

Open `native/OrbitCockpit/Package.swift` in Xcode.

## Security

The application has **Sandboxing disabled** (`com.apple.security.app-sandbox = NO`). This is intentional and necessary for:

- Creating PTY master/slave pairs.
- Spawning and controlling the `gh-orbit` child process.
- Direct access to local unix domain sockets.

The app relies on **Developer ID** signing for security.
