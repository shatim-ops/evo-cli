// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package floatingip is the one rule of the floating IP a deploy reserves
// for its VM — provision, deploy, deploy_apply and the re-run of a deploy
// that gives a VM its missing address: a create whose answer is lost is
// not sent again in the same call, and the call that follows looks for
// that address by its exact name before it asks to reserve another (D107;
// «Риск», conclusion 180, У1, У2 and У4).
//
// The address is billed from its create, and the list of addresses may
// not show it at once (a hypothesis: no run). A second create would
// reserve a second billed address while the first stays bound to nothing.
package floatingip

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// LostError is a create of a floating IP whose answer did not say whether
// the address was made, and that the list does not show by its name: the
// address may exist and be billed. Nothing more was sent.
type LostError struct {
	// Name is the exact name the create gave the address.
	Name string
	// Err is the error of the create.
	Err error
}

func (e *LostError) Error() string {
	return "floating IP " + e.Name + ": the create got no answer: " + e.Err.Error()
}

func (e *LostError) Unwrap() error { return e.Err }

// AskError is a call that would reserve an address for a VM while the
// address an earlier call asked for is not found by its name: whether to
// reserve another is a person's to say (Mark.Answered). Nothing was sent.
type AskError struct {
	Mark state.LostFloatingIP
}

func (e *AskError) Error() string {
	return "floating IP " + e.Mark.Name + ", requested " + e.Mark.RequestedAt.UTC().Format(time.RFC3339) +
		", got no answer and is not listed: a person decides whether to reserve another"
}

// AsLost and AsAsk unwrap the errors of this package.
func AsLost(err error) (*LostError, bool) {
	var e *LostError
	return e, stderrors.As(err, &e)
}

// AsAsk: see AsLost.
func AsAsk(err error) (*AskError, bool) {
	var e *AskError
	return e, stderrors.As(err, &e)
}

// Refused reports a create the API answered with a refusal: no address
// came of it, and another create is not a second address — a quota, or a
// status that says the request was not carried out
// (intent.DefiniteRefusal, the one rule of it).
func Refused(err error) bool {
	return err != nil && (provision.IsQuotaError(err) || intent.DefiniteRefusal(provision.HTTPStatus(err)))
}

// lookupAttempts and lookupWait bound the reads after a lost create: a
// create cut by the caller (its context done) may still be made a moment
// after the client hung up, so it is read three times.
const (
	lookupAttempts = 3
	lookupWait     = 30 * time.Second
)

// Reserve sends the create of req once. An answer with the address is the
// address; a refusal (Refused) is its error — the caller may send another.
// Any other outcome — no answer, a 5xx, an answer without an id, a cut —
// sends nothing more: the address is looked for by its exact name among
// those own takes (Lookup), and one not found is a LostError. interval is
// the pause between the reads of a cut call; say, when not nil, shows what
// the call does while it reads.
func Reserve(ctx context.Context, cloud provision.CloudAPI, req provision.CreateFloatingIPRequest,
	own func(provision.FloatingIP) bool, interval time.Duration, say func(string)) (*provision.FloatingIP, error) {
	fip, err := cloud.CreateFloatingIP(ctx, req)
	if err == nil && fip != nil && fip.ID != "" {
		return fip, nil
	}
	if Refused(err) {
		return nil, err
	}
	if err == nil {
		err = stderrors.New("the create answered no floating IP id")
	}
	if f := Lookup(ctx, cloud, req.ProjectID, req.Name, own, interval, say); f != nil {
		return f, nil
	}
	return nil, &LostError{Name: req.Name, Err: err}
}

// Lookup reads the address a create with no answer may have made, by its
// exact name (Find). The read outlives a cut of the call (its context
// done), and then is made three times, interval apart: the server may make
// the address a moment after the client hung up (review Grok on #85). It
// says so before it reads again.
func Lookup(ctx context.Context, cloud provision.CloudAPI, projectID, name string,
	own func(provision.FloatingIP) bool, interval time.Duration, say func(string)) *provision.FloatingIP {
	attempts := 1
	if ctx.Err() != nil {
		attempts = lookupAttempts
		if say != nil {
			say(i18n.Tf("Looking for floating IP %s made before the interruption...",
				"Ищу floating IP %s, созданный до прерывания...", name))
		}
	}
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lookupWait)
	defer cancel()
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-lctx.Done():
				return nil
			case <-time.After(interval):
			}
		}
		if f := Find(lctx, cloud, projectID, name, own); f != nil {
			return f
		}
	}
	return nil
}

// Find is the address of the project with exactly that name that own
// takes; nil when the list does not show one, or cannot be read.
func Find(ctx context.Context, cloud provision.CloudAPI, projectID, name string, own func(provision.FloatingIP) bool) *provision.FloatingIP {
	fips, err := cloud.ListFloatingIPs(ctx, projectID)
	if err != nil {
		return nil
	}
	for _, f := range fips {
		if f != nil && f.ID != "" && f.Name == name && (own == nil || own(*f)) {
			return f
		}
	}
	return nil
}

// Free takes an address bound to nothing: the one a create made for a VM
// not created yet (К1 of conclusion 180).
func Free(f provision.FloatingIP) bool { return !f.Bound() && !f.OnNATGateway() }

// FreeOrOf takes an address bound to nothing or to the VM (or its
// interface) of these ids: the one a create for that VM made.
func FreeOrOf(ids ...string) func(provision.FloatingIP) bool {
	return func(f provision.FloatingIP) bool {
		if Free(f) {
			return true
		}
		for _, id := range ids {
			if f.BoundTo(id) {
				return true
			}
		}
		return false
	}
}

// Note records the mark m of a lost create in sf; a mark of the same name
// in the same project is replaced.
func Note(sf *state.File, m state.LostFloatingIP) {
	Forget(sf, m.ProjectID, m.Name)
	sf.LostFloatingIPs = append(sf.LostFloatingIPs, m)
}

// Forget removes the mark of the address name in the project.
func Forget(sf *state.File, projectID, name string) {
	kept := sf.LostFloatingIPs[:0]
	for _, m := range sf.LostFloatingIPs {
		if m.ProjectID != projectID || m.Name != name {
			kept = append(kept, m)
		}
	}
	sf.LostFloatingIPs = kept
	if len(kept) == 0 {
		sf.LostFloatingIPs = nil
	}
}

// Answer records that a person answered the question about the mark of
// name: the mark stays (the first address may still come), and asks no
// more.
func Answer(sf *state.File, projectID, name string) {
	for i := range sf.LostFloatingIPs {
		if m := &sf.LostFloatingIPs[i]; m.ProjectID == projectID && m.Name == name {
			m.Answered = true
		}
	}
}

// Of are the marks of the VM in the project: by its id once it has one,
// else by its name — a mark made before the VM names only the name.
func Of(sf *state.File, projectID, vmName, vmID string) []state.LostFloatingIP {
	if sf == nil {
		return nil
	}
	var out []state.LostFloatingIP
	for _, m := range sf.LostFloatingIPs {
		if m.ProjectID != projectID {
			continue
		}
		// By the name alone when one side has no id: a mark made before the
		// VM, or a call that knows the VM by its name only (a plan).
		byName := vmName != "" && m.VMName == vmName && (m.VMID == "" || vmID == "")
		if (vmID != "" && m.VMID == vmID) || byName {
			out = append(out, m)
		}
	}
	return out
}

// Settle looks for the addresses the marks of the VM name, before a call
// reserves one for it: one listed by its exact name that own takes is the
// VM's — its mark goes, and the call takes it instead of a new one. A
// mark not found that no person has answered is the question (AskError):
// nothing is sent. Marks a person answered are kept and do not stop the
// call. sf is changed in memory; the caller writes it.
func Settle(ctx context.Context, cloud provision.CloudAPI, sf *state.File, projectID, vmName, vmID string,
	own func(provision.FloatingIP) bool) (*provision.FloatingIP, error) {
	marks := Of(sf, projectID, vmName, vmID)
	if len(marks) == 0 {
		return nil, nil
	}
	fips, err := cloud.ListFloatingIPs(ctx, projectID)
	if err != nil {
		fips = nil // not read: a mark not found asks, as one not listed
	}
	var ask *state.LostFloatingIP
	for _, m := range marks {
		for _, f := range fips {
			if f != nil && f.ID != "" && f.Name == m.Name && (own == nil || own(*f)) {
				Forget(sf, projectID, m.Name)
				return f, nil
			}
		}
		if !m.Answered && ask == nil {
			ask = &m
		}
	}
	if ask != nil {
		return nil, &AskError{Mark: *ask}
	}
	return nil, nil
}

// Apply makes in f the change of the marks from before to after: a mark
// gone is forgotten, a mark new or changed is noted. A call that read the
// state, changed its marks in memory and writes them back under the lock
// keeps the marks another process wrote in between.
func Apply(f *state.File, before, after []state.LostFloatingIP) {
	key := func(m state.LostFloatingIP) string { return m.ProjectID + "\x00" + m.Name }
	now := map[string]bool{}
	for _, m := range after {
		now[key(m)] = true
	}
	for _, m := range before {
		if !now[key(m)] {
			Forget(f, m.ProjectID, m.Name)
		}
	}
	was := map[string]state.LostFloatingIP{}
	for _, m := range before {
		was[key(m)] = m
	}
	for _, m := range after {
		if old, ok := was[key(m)]; !ok || !sameMark(old, m) {
			Note(f, m)
		}
	}
}

func sameMark(a, b state.LostFloatingIP) bool {
	return a.Name == b.Name && a.ProjectID == b.ProjectID && a.VMName == b.VMName && a.VMID == b.VMID &&
		a.RequestedAt.Equal(b.RequestedAt) && a.Answered == b.Answered
}

// Release is the cleanup of the marks of a VM being destroyed: the
// address of each, listed by its exact name free or on the VM, is released
// — evo made it, and nothing else holds it. A mark whose address the list
// does not show is closed and returned in missing: the answer names it,
// for the person to release it if it comes (Т3 and Т4 of conclusion 180).
// A mark whose release failed stays. A list that cannot be read keeps
// every mark and is the error. sf is changed in memory.
func Release(ctx context.Context, cloud provision.CloudAPI, sf *state.File, projectID, vmName, vmID string) (released, missing []state.LostFloatingIP, err error) {
	marks := Of(sf, projectID, vmName, vmID)
	if len(marks) == 0 {
		return nil, nil, nil
	}
	fips, err := cloud.ListFloatingIPs(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	own := FreeOrOf(vmID)
	var failed error
	for _, m := range marks {
		var found *provision.FloatingIP
		for _, f := range fips {
			if f != nil && f.ID != "" && f.Name == m.Name && own(*f) {
				found = f
				break
			}
		}
		if found == nil {
			missing = append(missing, m)
			Forget(sf, projectID, m.Name)
			continue
		}
		if derr := cloud.DeleteFloatingIP(ctx, found.ID, projectID); derr != nil && provision.HTTPStatus(derr) != 404 {
			failed = stderrors.Join(failed, derr)
			continue
		}
		released = append(released, m)
		Forget(sf, projectID, m.Name)
	}
	return released, missing, failed
}
