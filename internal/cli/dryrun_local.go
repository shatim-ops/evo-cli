// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"os"

	"github.com/shatim-ops/evo-cli/internal/confirm"
)

// dryRunWrite is --dry-run of a local command that writes: the preview of
// what it would write, and true — the command stops there. One point for
// every such command (audit of 1.8, A18-3: logout, configure delete,
// configure, configure s3 and diag on wrote under --dry-run --yes).
func dryRunWrite(ctx context.Context, p confirm.Preview) bool {
	if !flagDryRun {
		return false
	}
	_, _ = confirm.Ask(ctx, p, confirm.Options{DryRun: true, JSON: flagJSON, Out: os.Stdout, Err: os.Stderr})
	return true
}
