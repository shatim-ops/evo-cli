// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/verify"
	"github.com/shatim-ops/evo-cli/internal/vmstage"
)

// Engine orchestrates all CLI operations.
type Engine struct {
	Config    config.Config
	Auth      auth.TokenProvider
	Cloud     provision.CloudAPI
	Deployer  deploy.Deployer
	Printer   *output.Printer
	SSHKeyPEM []byte // Ed25519 private key for SSH
	SSHPubKey string // Ed25519 public key for image metadata
	StateDir  string // project root (for state file)

	// SSHKeyLoader loads or creates the key pair on first use (EnsureSSHKey),
	// so commands that never reach a VM leave no key behind.
	SSHKeyLoader func() (privPEM []byte, pubKey string, err error)

	// Timings overrides poll/retry waits; zero fields keep production defaults.
	Timings Timings
	// QuotaProbe reads the project's quotas for the preview of a VM (D60):
	// nil — the preview says nothing of them; ErrQuotasForbidden — the key
	// reads none.
	QuotaProbe func(ctx context.Context, tp auth.TokenProvider, projectID string) error
	// Probe runs post-deploy health checks; nil means verify.Check.
	Probe func(ctx context.Context, ip string, ports []int, timeout time.Duration) []verify.ProbeResult
	// Stages runs the long stages of deploy on the VM (vmstage); nil: the
	// Deployer, when it runs stages itself, else vmstage.Over(Deployer).
	Stages vmstage.Runner

	// until is DeployOptions.Until of the deploy that runs.
	until time.Time
	// boundFrom is when that deploy began: the bound's length is
	// until - boundFrom.
	boundFrom time.Time
	// phase is the step of the deploy that runs, as InProgress names it:
	// a step cut by the bound is going on from there (atBound).
	phase string
}

// DeployOptions holds parameters for the deploy command.
type DeployOptions struct {
	ComposeFile    string
	Flavor         string // "auto" or explicit flavor name
	Region         string
	Force          bool // force redeploy even if hash unchanged
	NonInteractive bool // never prompt; creating a VM then needs Yes
	Yes            bool // consent to create the VM given in advance (--yes)
	DryRun         bool // show what would be created and its cost, then stop
	// Gate, when set, asks for consent where Deploy would call confirm.Ask
	// with its own preview: the deploy tool sets it for 'evo deploy', which
	// shows the preview of the tool's answer (spec REQ-4, REQ-6). redeploy
	// is the engine's words for an update of the existing VM, nil for a new
	// VM, whose preview the tool makes itself.
	Gate func(ctx context.Context, redeploy *confirm.Preview) (bool, error)
	// Until bounds the waits of deploy for the cloud and the VM (D69: the
	// budget of an MCP call); zero, the CLI: none. Past it, Deploy returns
	// *InProgress with what goes on, and the stages on the VM go on.
	Until time.Time
}

// EnsureSSHKey fills SSHKeyPEM/SSHPubKey from SSHKeyLoader unless they are
// already set. Commands that connect to the VM call it before they need the key.
func (e *Engine) EnsureSSHKey() error {
	if len(e.SSHKeyPEM) > 0 || e.SSHKeyLoader == nil {
		return nil
	}
	priv, pub, err := e.SSHKeyLoader()
	if err != nil {
		return fmt.Errorf(i18n.T("SSH key: %w", "SSH-ключ: %w"), err)
	}
	e.SSHKeyPEM, e.SSHPubKey = priv, pub
	return nil
}
