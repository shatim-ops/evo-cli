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
	"crypto/rand"
	stderrors "errors"
	"slices"
	"strconv"
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
	Attempt string // description sent in this create
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
		", got no answer and its address could not be identified: a person decides whether to reserve another"
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
// those matching both the request description and own. A LostError keeps
// the attempt for a later check. interval is
// the pause between the reads of a cut call; say, when not nil, shows what
// the call does while it reads.
func Reserve(ctx context.Context, cloud provision.CloudAPI, req provision.CreateFloatingIPRequest,
	own func(provision.FloatingIP) bool, interval time.Duration, say func(string)) (*provision.FloatingIP, error) {
	req.Description = "evo-fip-" + rand.Text()
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
	match := func(f provision.FloatingIP) bool { return f.Description == req.Description && (own == nil || own(f)) }
	if f := Lookup(ctx, cloud, req.ProjectID, req.Name, match, interval, say); f != nil {
		return f, nil
	}
	return nil, &LostError{Name: req.Name, Attempt: req.Description, Err: err}
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
// takes; nil when the list does not show exactly one match, or cannot be read.
func Find(ctx context.Context, cloud provision.CloudAPI, projectID, name string, own func(provision.FloatingIP) bool) *provision.FloatingIP {
	fips, err := cloud.ListFloatingIPs(ctx, projectID)
	if err != nil {
		return nil
	}
	return uniqueAddress(fips, name, own)
}

// A single create cannot own two distinct IDs. Refuse an ambiguous list.
func uniqueAddress(fips []*provision.FloatingIP, name string, own func(provision.FloatingIP) bool) *provision.FloatingIP {
	var found *provision.FloatingIP
	for _, f := range fips {
		if f == nil || f.ID == "" || f.Name != name || (own != nil && !own(*f)) {
			continue
		}
		if found != nil && f.ID != found.ID {
			return nil
		}
		found = f
	}
	return found
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

// A name may be reused by another attempt, including by another process.
func receiptKey(m state.LostFloatingIP) [3]string {
	return [3]string{m.ProjectID, m.Name, m.Attempt}
}

// Note records evidence for this attempt, preserving evidence already saved.
func Note(sf *state.File, m state.LostFloatingIP) {
	for i, prior := range sf.LostFloatingIPs {
		if receiptKey(prior) == receiptKey(m) {
			if prior.ID != "" {
				m.ID = prior.ID
			}
			m.Answered = m.Answered || prior.Answered
			sf.LostFloatingIPs[i] = m
			return
		}
	}
	sf.LostFloatingIPs = append(sf.LostFloatingIPs, m)
}

// Forget removes only the receipt that has been reconciled.
func Forget(sf *state.File, mark state.LostFloatingIP) {
	sf.LostFloatingIPs = slices.DeleteFunc(sf.LostFloatingIPs, func(m state.LostFloatingIP) bool {
		return receiptKey(m) == receiptKey(mark)
	})
	if len(sf.LostFloatingIPs) == 0 {
		sf.LostFloatingIPs = nil
	}
}

// Answer records that a person answered the question about the mark of
// name: the mark stays (the first address may still come), and asks no
// more.
func Answer(sf *state.File, projectID, name string) {
	for i := range sf.LostFloatingIPs {
		if m := &sf.LostFloatingIPs[i]; m.ProjectID == projectID && m.Name == name && !m.Answered {
			m.Answered = true
			return
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

// Settle recovers one address whose description matches the local create
// receipt. Without this evidence the mark stays for a person to reconcile.
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
		if f := addressOfMark(fips, m, own); f != nil {
			Forget(sf, m)
			return f, nil
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

// A cloud name can belong to someone else, including for a legacy mark.
func addressOfMark(fips []*provision.FloatingIP, m state.LostFloatingIP, own func(provision.FloatingIP) bool) *provision.FloatingIP {
	if m.Attempt == "" && m.ID == "" {
		return nil
	}
	return uniqueAddress(fips, m.Name, func(f provision.FloatingIP) bool {
		if own != nil && !own(f) {
			return false
		}
		if m.ID != "" {
			return f.ID == m.ID
		}
		return f.Description == m.Attempt
	})
}

// Apply makes in f the change of the marks from before to after: a mark
// gone is forgotten, a mark new or changed is noted. A call that read the
// state, changed its marks in memory and writes them back under the lock
// keeps the marks another process wrote in between.
func Apply(f *state.File, before, after []state.LostFloatingIP) {
	now := map[[3]string]bool{}
	for _, m := range after {
		now[receiptKey(m)] = true
	}
	for _, m := range before {
		if !now[receiptKey(m)] {
			Forget(f, m)
		}
	}
	was := map[[3]string]state.LostFloatingIP{}
	for _, m := range before {
		was[receiptKey(m)] = m
	}
	for _, m := range after {
		if old, ok := was[receiptKey(m)]; !ok || !sameMark(old, m) {
			Note(f, m)
		}
	}
}

func sameMark(a, b state.LostFloatingIP) bool {
	return a.Name == b.Name && a.ProjectID == b.ProjectID && a.VMName == b.VMName && a.VMID == b.VMID &&
		a.RequestedAt.Equal(b.RequestedAt) && a.Answered == b.Answered && a.Attempt == b.Attempt && a.ID == b.ID
}

// Release cleans up addresses identified by the create attempt. Once an ID is
// established, further attempts and the final GET always use that same ID.
func Release(ctx context.Context, cloud provision.CloudAPI, sf *state.File, projectID, vmName, vmID, keep string) (released, missing []state.LostFloatingIP, err error) {
	marks := Of(sf, projectID, vmName, vmID)
	var fips []*provision.FloatingIP
	var listErr error
	if slices.ContainsFunc(marks, func(m state.LostFloatingIP) bool { return m.ID == "" }) {
		fips, listErr = cloud.ListFloatingIPs(ctx, projectID)
	}
	err = listErr
	for _, m := range marks {
		if m.ID == "" {
			if listErr != nil {
				continue
			}
			f := addressOfMark(fips, m, FreeOrOf(vmID))
			if f == nil {
				if !slices.ContainsFunc(fips, func(f *provision.FloatingIP) bool { return f != nil && f.Name == m.Name }) {
					missing = append(missing, m)
				}
				err = stderrors.Join(err, stderrors.New(i18n.Tf("floating IP %q: could not confirm which address this request created; check its ID in the cloud before releasing it", "floating IP %q: не удалось подтвердить, какой адрес создал этот запрос. Перед освобождением сверьте ID в облаке", m.Name)))
				continue
			}
			m.ID = f.ID
			Note(sf, m)
		}
		if releaseErr := releaseVerified(ctx, cloud, m, vmID, keep); releaseErr != nil {
			err = stderrors.Join(err, releaseErr)
			continue
		}
		released = append(released, m)
		Forget(sf, m)
	}
	return released, missing, err
}

func releaseVerified(ctx context.Context, cloud provision.CloudAPI, m state.LostFloatingIP, vmID, keep string) error {
	// A retry still owns the ID in its receipt, but it may now be in use
	// by another VM or gateway. Check the card before sending the DELETE.
	f, err := cloud.GetFloatingIP(ctx, m.ID)
	if NotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if f == nil || f.ID != m.ID || f.OnNATGateway() || !FreeOrOf(vmID)(*f) {
		return stderrors.New(i18n.Tf("could not confirm floating IP %q is free or belongs to this VM; its receipt was kept", "не удалось подтвердить, что floating IP %q свободен или принадлежит этой ВМ. Квитанция сохранена", m.Name))
	}
	if m.ID != keep {
		if err := cloud.DeleteFloatingIP(ctx, m.ID, m.ProjectID); err != nil && !NotFound(err) {
			return err
		}
	}
	gone, err := Gone(ctx, cloud, m.ID)
	if err != nil {
		return err
	}
	if !gone {
		return stderrors.New(i18n.Tf("floating IP %q is still present; retry destroy", "floating IP %q ещё существует. Повторите destroy", m.Name))
	}
	return nil
}

// NameFor is the name a create of an address for a VM gives it: base, or —
// when a mark of the project already holds base (a person said yes to one
// more while the first may still come) — base with the time after it, so
// two addresses never bear one name and each mark finds only its own
// («Риск», conclusion 205).
func NameFor(sf *state.File, projectID, base string, now time.Time) string {
	name := base
	for n := 0; sf != nil; n++ {
		used := false
		for _, m := range sf.LostFloatingIPs {
			if m.ProjectID == projectID && m.Name == name {
				used = true
				break
			}
		}
		if !used {
			break
		}
		suffix := "-" + strconv.FormatInt(now.Unix(), 36)
		if n > 0 {
			suffix += "-" + strconv.Itoa(n)
		}
		name = provision.FitName(base, suffix)
	}
	return name
}
