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
	// Probe runs post-deploy health checks; nil means verify.Check.
	Probe func(ctx context.Context, ip string, ports []int, timeout time.Duration) []verify.ProbeResult
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
