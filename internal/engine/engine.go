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
	"github.com/shatim-ops/evo-cli/internal/sgrules"
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
	// SSHDial probes port 22 of a VM before the question of an update
	// (ReachSSH, M2): DialSSH in the CLI and the server; nil, no probe.
	SSHDial func(ctx context.Context, addr string) error
	// sshReached remembers ReachSSH per address within one call.
	sshReached map[string]error
	// zones is the zone list of the call, once read (zoneList).
	zones []*provision.AvailabilityZone
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
	// portsOnlyFromGroups are the ports the last openPorts found let in
	// only from other security groups: the answer of deploy names them.
	portsOnlyFromGroups []sgrules.GroupOnlyPort
	// now is the clock of the bound (past); nil: time.Now. A test brings
	// the bound at a point of the deploy with it, not by the wall clock.
	now func() time.Time
	// decided: the error deploy returns was decided by atBound, and the
	// record was written by that decision; the deferred atBound keeps it
	// (FB171-4: two readings of the clock gave the record one answer and
	// the caller another).
	decided bool
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
	// Cidr is the source the rules of the ports deploy opens let in
	// (sgrules.Source: 0.0.0.0/0 when empty, P5); rules already there
	// stay as they are.
	Cidr string
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
