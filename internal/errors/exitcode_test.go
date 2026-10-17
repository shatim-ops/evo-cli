package errors_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func TestExitCode(t *testing.T) {
	cli := func(code string) error { return clierrors.New(code, "msg", "hint") }

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain error", fmt.Errorf("boom"), 1},
		{"usage", cli(clierrors.CodeUsage), 1},
		{"auth required", cli(clierrors.CodeAuthRequired), 2},
		{"auth expired", cli(clierrors.CodeAuthExpired), 2},
		{"auth failed", cli(clierrors.CodeAuthFailed), 2},
		{"config invalid", cli(clierrors.CodeConfigInvalid), 2},
		{"confirmation required", cli(clierrors.CodeConfirmationRequired), 3},
		{"not found", cli(clierrors.CodeVMNotFound), 4},
		{"other object not found", cli(clierrors.CodeNotFound), 4},
		{"MCP server busy", cli(clierrors.CodeServerBusy), 1},
		{"api error", cli(clierrors.CodeAPIError), 5},
		{"rate limited", cli(clierrors.CodeAPIRateLimited), 5},
		{"api unreachable", cli(clierrors.CodeAPIUnreachable), 1},
		{"ssh timeout", cli(clierrors.CodeSSHTimeout), 1},
		{"state corrupted", cli(clierrors.CodeStateCorrupted), 1},
		{"wrapped auth", fmt.Errorf("list zones: %w", cli(clierrors.CodeAuthRequired)), 2},
		{"double wrapped not found", fmt.Errorf("a: %w", fmt.Errorf("b: %w", cli(clierrors.CodeVMNotFound))), 4},
		{"context canceled", context.Canceled, 130},
		{"canceled inside url.Error inside CLIError", clierrors.Wrap(clierrors.CodeAPIUnreachable, "cannot reach", "",
			&url.Error{Op: "Get", URL: "https://x", Err: context.Canceled}), 130},
		{"deadline exceeded is not an interrupt", context.DeadlineExceeded, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clierrors.ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
