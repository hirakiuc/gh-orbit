package api

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
)

var (
	// Trusted command names
	reAllowedCmd = regexp.MustCompile(`^(gh|osascript|sysctl|defaults)$`)
	
	// Argument safety regexes (shared internal validation)
	rePRNumber   = regexp.MustCompile(`^[0-9]+$`)
	reRepoName   = regexp.MustCompile(`^[a-zA-Z0-9-._]+/[a-zA-Z0-9-._]+$`)
	reTagName    = regexp.MustCompile(`^[a-zA-Z0-9-._/]+$`)
)

type realCommandExecutor struct{}

// NewCommandExecutor returns a production implementation of the CommandExecutor.
func NewCommandExecutor() CommandExecutor {
	return &realCommandExecutor{}
}

func (e *realCommandExecutor) validateArgs(args []string) error {
	for _, arg := range args {
		if len(arg) > 0 && arg[0] != '-' {
			if strings.Contains(arg, "/") {
				if !reRepoName.MatchString(arg) && !reTagName.MatchString(arg) {
					return fmt.Errorf("invalid repository or tag: %s", arg)
				}
			} else if rePRNumber.MatchString(arg) {
				continue
			}
		}
	}
	return nil
}

func (e *realCommandExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	if !reAllowedCmd.MatchString(name) {
		return nil, fmt.Errorf("refusing to execute untrusted command: %s", name)
	}
	if name == "gh" {
		if err := e.validateArgs(args); err != nil {
			return nil, err
		}
	}

	// #nosec G204: name is validated against a strict whitelist
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("command %s failed: %w", name, err)
	}
	return out, nil
}

func (e *realCommandExecutor) Run(ctx context.Context, name string, args ...string) error {
	if !reAllowedCmd.MatchString(name) {
		return fmt.Errorf("refusing to execute untrusted command: %s", name)
	}
	if name == "gh" {
		if err := e.validateArgs(args); err != nil {
			return err
		}
	}

	// #nosec G204: name is validated against a strict whitelist
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command %s failed: %w", name, err)
	}
	return nil
}

func (e *realCommandExecutor) InteractiveGH(callback func(error) tea.Msg, args ...string) tea.Cmd {
	if err := e.validateArgs(args); err != nil {
		return func() tea.Msg { return errMsg{err: err} }
	}

	// #nosec G204: 'gh' is a trusted constant name
	c := exec.Command("gh", args...)
	return tea.ExecProcess(c, callback)
}

// errMsg is a copy of TUI errMsg to allow returning errors from InteractiveGH without circular dependency.
type errMsg struct{ err error }
