# GitHub CLI Extension Guidelines (Go focus)

## Checklist: Before Distributing

- [ ] Repository is named `gh-orbit`.
- [ ] Executable binary follows the naming pattern `gh-orbit-OS-ARCH`.
- [ ] Windows assets end with `.exe`.
- [ ] GitHub Actions are configured to attach binaries to a release.

## Code Pattern: Zero-Config Auth (Go)

Use the `go-gh` library to interact with the GitHub API. This is the idiomatic way to inherit the user's `gh` token.

```go
import (
 "github.com/cli/go-gh/v2"
 "github.com/cli/go-gh/v2/pkg/api"
)

func main() {
 client, err := gh.RESTClient(nil)
 if err != nil {
  log.Fatal(err)
 }
 // Use the client to make authenticated requests
}
```

## UI/UX Best Practices

- **Terminal Size**: Bubble Tea applications should handle window resizing gracefully.
- **Machine Readability**: Use `--json` when fetching data.
- **Non-interactive**: Avoid prompts when running as a script (use `--title`, `--body`, etc.).
