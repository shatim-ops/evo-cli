// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// Production waits used by Deploy and Destroy. Engine.Timings overrides them per field.
const (
	vmPollInterval        = 5 * time.Second
	vmPollTimeout         = config.DefaultTimeout * time.Second // CLOUDRU_TIMEOUT's default (25 min)
	vmProgressInterval    = 30 * time.Second                    // a "waiting for the VM" line without a terminal
	vmLiveRefresh         = time.Second                         // the timer of that line on a terminal
	ifacePollInterval     = 5 * time.Second
	fipRetryInterval      = 10 * time.Second
	fipLookupInterval     = 2 * time.Second // after Ctrl-C: short, before a second Ctrl-C
	fipPollInterval       = 5 * time.Second
	fipSettleTimeout      = time.Minute // a FIP still being created, before the VM create and its release (cloud-e2e #121)
	sshRetryInterval      = 5 * time.Second
	verifySettleDelay     = 2 * time.Second
	verifyTimeout         = 10 * time.Second
	dockerPollInterval    = 10 * time.Second
	dockerWaitTimeout     = 15 * time.Minute // cloud-init, then the image mirror and, when it fails, the official one
	sshSlowNote           = 5 * time.Minute  // a new VM usually answers SSH in 2-5 minutes
	destroyPollInterval   = 3 * time.Second
	destroyPollTimeout    = 2 * time.Minute  // unconfirmed is a partial destroy to re-run (release audit, A-005)
	deletableWaitTimeout  = 10 * time.Minute // a VM still "creating" cannot be deleted yet
	deletablePollInterval = 10 * time.Second
	statusRefreshTimeout  = 8 * time.Second // status answers even when Cloud.ru does not
	ifaceMaxAttempts      = 24              // × ifacePollInterval ≈ 2 minutes
	ifaceFallbackAttempt  = 12              // after this, accept a primary interface in any state
)

// Timings holds the waits Deploy and Destroy use between polls and retries. A zero field
// means the production default from the constants above; tests set
// milliseconds so that no test sleeps for real.
type Timings struct {
	VMPollInterval time.Duration // GetVM polling until the VM is active
	VMPollTimeout  time.Duration // give up waiting for the VM to become active
	// VMProgressInterval is how often deploy prints "waiting for the VM…"
	// without a terminal; on a terminal one line is redrawn every second.
	VMProgressInterval time.Duration
	IfacePollInterval  time.Duration // waiting for the primary interface to be "created"
	FIPRetryInterval   time.Duration // between CreateFloatingIP attempts
	FIPLookupInterval  time.Duration // between lookups of a boot FIP after Ctrl-C
	FIPPollInterval    time.Duration // waiting for the floating IP to become in_use
	FIPSettleTimeout   time.Duration // a floating IP still being created: ready for the VM create, deletable
	SSHRetryInterval   time.Duration // between SSH connect attempts
	SSHSlowNote        time.Duration // without SSH this long, deploy says once why it still waits
	SettleDelay        time.Duration // pause before health checks after compose up
	VerifyTimeout      time.Duration // per-port HTTP probe timeout

	DockerPollInterval  time.Duration // between checks that cloud-init has installed Docker
	DockerWaitTimeout   time.Duration // give up waiting for Docker on a new VM
	DestroyPollInterval time.Duration // GetVM polling until a deleted VM is gone
	DestroyPollTimeout  time.Duration // stop waiting for the deletion to be confirmed

	DeletableWaitTimeout  time.Duration // destroy: how long to retry while the VM cannot be deleted yet
	DeletablePollInterval time.Duration // destroy: between those retries

	StatusRefreshTimeout time.Duration // status: the whole GET of the VM, authentication included
}

// withDefaults fills zero fields with the production defaults.
func (t Timings) withDefaults() Timings {
	def := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&t.VMPollInterval, vmPollInterval)
	def(&t.VMPollTimeout, vmPollTimeout)
	def(&t.VMProgressInterval, vmProgressInterval)
	def(&t.IfacePollInterval, ifacePollInterval)
	def(&t.FIPRetryInterval, fipRetryInterval)
	def(&t.FIPLookupInterval, fipLookupInterval)
	def(&t.FIPPollInterval, fipPollInterval)
	def(&t.FIPSettleTimeout, fipSettleTimeout)
	def(&t.SSHRetryInterval, sshRetryInterval)
	def(&t.SSHSlowNote, sshSlowNote)
	def(&t.SettleDelay, verifySettleDelay)
	def(&t.VerifyTimeout, verifyTimeout)
	def(&t.DockerPollInterval, dockerPollInterval)
	def(&t.DockerWaitTimeout, dockerWaitTimeout)
	def(&t.DestroyPollInterval, destroyPollInterval)
	def(&t.DestroyPollTimeout, destroyPollTimeout)
	def(&t.DeletableWaitTimeout, deletableWaitTimeout)
	def(&t.DeletablePollInterval, deletablePollInterval)
	def(&t.StatusRefreshTimeout, statusRefreshTimeout)
	return t
}

func (e *Engine) timings() Timings {
	return e.Timings.withDefaults()
}

// VMPollTimeout is how long a new VM is waited for to become active:
// Timings.VMPollTimeout (CLOUDRU_TIMEOUT), else the default of every
// surface (config.DefaultTimeout). deploy and provision both wait this long.
func (e *Engine) VMPollTimeout() time.Duration { return e.timings().VMPollTimeout }

// newPollTicker is time.NewTicker that also ticks at once: the first check
// of a VM that is already active (or already gone) should not wait a full
// interval — that cost deploy 5 s and destroy 3 s every time. Like a
// ticker, it drops ticks nobody reads. stop releases it.
func newPollTicker(d time.Duration) (c <-chan time.Time, stop func()) {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	t := time.NewTicker(d)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case tick := <-t.C:
				select {
				case ch <- tick:
				default:
				}
			case <-done:
				return
			}
		}
	}()
	return ch, func() { t.Stop(); close(done) }
}
