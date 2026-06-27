package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

const reviewWorkspaceRequestDirectoryEnv = "GH_ORBIT_REVIEW_WORKSPACE_REQUEST_DIR"

func (a *MCPAdapter) StartReviewWorkspace(ctx context.Context, request types.ReviewWorkspaceStartRequest) error {
	if err := validateReviewWorkspaceStartRequest(request); err != nil {
		return err
	}

	requestDirectory := strings.TrimSpace(os.Getenv(reviewWorkspaceRequestDirectoryEnv))
	if requestDirectory == "" {
		return types.ErrReviewWorkspaceUnsupported
	}

	// #nosec G703 -- requestDirectory comes from the Cockpit-managed local launch environment.
	if err := os.MkdirAll(requestDirectory, 0o700); err != nil {
		return fmt.Errorf("prepare review workspace request directory: %w", err)
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode review workspace request: %w", err)
	}
	payload = append(payload, '\n')

	tempFile, err := os.CreateTemp(requestDirectory, "review-workspace-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create review workspace request: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		// #nosec G703 -- tempPath is created within the trusted request directory above.
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(payload); err != nil {
		return fmt.Errorf("write review workspace request: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close review workspace request: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	finalPath := filepath.Join(
		requestDirectory,
		fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), uuid.NewString()),
	)
	// #nosec G703 -- both paths are created within the trusted request directory above.
	if err := os.Rename(tempPath, finalPath); err != nil {
		return fmt.Errorf("publish review workspace request: %w", err)
	}

	return nil
}

func validateReviewWorkspaceStartRequest(request types.ReviewWorkspaceStartRequest) error {
	switch {
	case strings.TrimSpace(request.Repository.Host) == "":
		return fmt.Errorf("%w: missing repository host", types.ErrInvalidReviewWorkspaceRequest)
	case strings.TrimSpace(request.Repository.Owner) == "":
		return fmt.Errorf("%w: missing repository owner", types.ErrInvalidReviewWorkspaceRequest)
	case strings.TrimSpace(request.Repository.Name) == "":
		return fmt.Errorf("%w: missing repository name", types.ErrInvalidReviewWorkspaceRequest)
	case request.PullRequestNumber <= 0:
		return fmt.Errorf("%w: invalid pull request number", types.ErrInvalidReviewWorkspaceRequest)
	default:
		return nil
	}
}
