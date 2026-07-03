# Native Settings Behavior

Orbit Cockpit’s native settings are organized by how they apply, not just by where they appear in the UI.

## Live-Applied Settings

These settings update already-running SwiftTerm panes without restarting the session.

- Font size
- Nerd Font preference
- Bright colors for bold text
- Custom block glyph settings
- Terminal theme
- Option-as-Meta behavior
- Mouse reporting
- Backspace sends Control-H

User-facing rule:

- Changes take effect immediately for running panes.
- New terminal sessions also start with the updated values.

## New-Session-Only Settings

These settings are applied when a terminal session is created.

- Scrollback line limit
- Cursor style
- `TERM` value
- Tab width
- Screen reader mode
- Sixel support advertisement
- ANSI 256-color palette strategy

User-facing rule:

- Changes apply to new terminal sessions.
- Existing panes keep the startup configuration they were launched with.

Orbit Cockpit intentionally does not pretend these settings update already-running panes, because SwiftTerm treats them as startup-oriented terminal options.

## Renderer / GPU Settings

These settings control how SwiftTerm renders the terminal view.

- Metal renderer enable/disable
- Metal buffering mode

User-facing rule:

- Orbit Cockpit applies these settings to running panes when SwiftTerm can switch safely.
- Metal activation is deferred until the terminal view is attached to a window.
- If Metal activation fails or Metal is unavailable, the pane stays usable on the CoreGraphics fallback path.

This means the saved preference and the active renderer can differ temporarily or permanently on unsupported systems. The app treats that as a supported fallback case, not as a successful Metal activation.

## Persistence Model

All native settings are persisted in the native settings store and survive relaunch.

The current implementation keeps three behavior-oriented paths separate:

- `TerminalSessionSettings`: runtime-safe live updates
- `TerminalStartupSettings`: new-session-only startup configuration
- `TerminalRendererSettings`: renderer preferences and fallback-sensitive behavior

That separation is intentional. It prevents Orbit Cockpit from claiming a setting applies immediately when the underlying terminal only supports it during session creation or through a guarded renderer transition.
