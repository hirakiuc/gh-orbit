package api

import (
	"context"
	"os/exec"

	"github.com/hirakiuc/gh-orbit/internal/types"
)

// OSCommandExecutor executes system commands using os/exec.
type OSCommandExecutor struct{}

func NewOSCommandExecutor() *OSCommandExecutor {
	return &OSCommandExecutor{}
}

func (e *OSCommandExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	// #nosec G204: Intentional dynamic command execution for system integration
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

func (e *OSCommandExecutor) Run(ctx context.Context, name string, args ...string) error {
	// #nosec G204: Intentional dynamic command execution for system integration
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

// Ensure OSCommandExecutor implements CommandExecutor
var _ types.CommandExecutor = (*OSCommandExecutor)(nil)
