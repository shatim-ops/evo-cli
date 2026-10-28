// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package sgrules opens a deployment's ports in its security group and
// closes them again on destroy. The group is shared by every VM placed in
// it, so a deployment records only the rules it created itself (by ID), and
// destroy leaves a rule alone while another deployment in the same state
// still needs that port.
//
// API (Cloud.ru «Виртуальные машины» OpenAPI v3): POST
// /api/v1/security-groups/{id}/rules answers 201 with the rule and its id;
// DELETE /api/v1/security-groups/{id}/rules/{rule_id} answers 204, 404 when
// the rule is gone.
package sgrules

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// PortKey is how ports are recorded in state: "80/tcp", "53/udp".
func PortKey(proto string, port int) string {
	return fmt.Sprintf("%d/%s", port, proto)
}

// Label is the short form shown to people: "80" for tcp, "53/udp".
func Label(key string) string {
	return strings.TrimSuffix(key, "/tcp")
}

func splitKey(key string) (proto string, port int) {
	p, proto, _ := strings.Cut(key, "/")
	port, _ = strconv.Atoi(p)
	return proto, port
}

// ParsePorts reads port specs as people write them: "80", "8080/tcp",
// "53/udp". Duplicates are dropped; a bad spec is a USAGE error.
func ParsePorts(specs []string) (tcp, udp []int, err error) {
	seen := map[string]bool{}
	for _, raw := range specs {
		spec := strings.ToLower(strings.TrimSpace(raw))
		if spec == "" {
			continue
		}
		num, proto, hasProto := strings.Cut(spec, "/")
		if !hasProto {
			proto = "tcp"
		}
		port, perr := strconv.Atoi(strings.TrimSpace(num))
		if perr != nil || port < 1 || port > 65535 || (proto != "tcp" && proto != "udp") {
			return nil, nil, clerrors.New(clerrors.CodeUsage,
				i18n.Tf("bad port %q", "неверный порт %q", raw),
				i18n.T("A port is 1-65535, optionally with /tcp or /udp: 80, 8080/tcp, 53/udp.",
					"Порт — число 1–65535, можно с /tcp или /udp: 80, 8080/tcp, 53/udp."))
		}
		key := PortKey(proto, port)
		if seen[key] {
			continue
		}
		seen[key] = true
		if proto == "udp" {
			udp = append(udp, port)
		} else {
			tcp = append(tcp, port)
		}
	}
	return tcp, udp, nil
}

// OpenResult says what Open did, by port label.
type OpenResult struct {
	Opened      []string `json:"opened,omitempty"`
	AlreadyOpen []string `json:"already_open,omitempty"`
	Failed      []string `json:"failed,omitempty"`
	// Untracked rules were created, but the API returned no rule ID and the
	// rule was not found afterwards: destroy cannot close them.
	Untracked []string `json:"untracked,omitempty"`
}

// inactive rule states: such a rule does not (or soon will not) open a port.
var inactive = map[string]bool{"deleting": true, "error": true, "error_creating": true, "error_deleting": true}

// Coverage reports whether a rule of rules already lets proto/port in from
// cidr, and whether it does so wider than asked (a range around the port,
// any protocol, a wider source) — one answer for deploy (Open) and
// open_ports, before the question and after it (D52, FB15-16). A rule
// counts when it is an active ingress rule of the protocol or of any,
// its range holds the port, and its source prefix holds cidr. A rule whose
// answer names no source prefix counts as before D52: what the API sends
// for «from anywhere» is not verified live, and a second rule for every
// port of every deploy would cost more than the gap (no field — as before).
func Coverage(rules []*provision.SGRule, proto string, port int, cidr string) (covered, wider bool) {
	want, err := netip.ParsePrefix(cidr)
	if err != nil {
		// A bare address is the prefix of that one address.
		a, aerr := netip.ParseAddr(cidr)
		if aerr != nil {
			return false, false
		}
		want = netip.PrefixFrom(a, a.BitLen())
	}
	exact := fmt.Sprintf("%d:%d", port, port)
	for _, r := range rules {
		if r == nil || r.Direction != "ingress" || inactive[r.State] {
			continue
		}
		rp := strings.ToLower(r.IPProtocol)
		if rp != proto && rp != "" && rp != "any" {
			continue
		}
		lo, hi, ok := portRange(r.PortRange)
		if !ok || port < lo || port > hi {
			continue
		}
		sameSource := r.RemoteIPPrefix == ""
		if !sameSource {
			src, err := netip.ParsePrefix(r.RemoteIPPrefix)
			if err != nil || src.Bits() > want.Bits() || !src.Contains(want.Addr()) {
				continue
			}
			sameSource = src.Bits() == want.Bits()
		}
		if rp == proto && r.PortRange == exact && sameSource {
			return true, false
		}
		covered, wider = true, true
	}
	return covered, wider
}

// portRange reads a rule's "from:to" (or one port).
func portRange(s string) (lo, hi int, ok bool) {
	a, b, found := strings.Cut(s, ":")
	if !found {
		b = a
	}
	lo, err1 := strconv.Atoi(strings.TrimSpace(a))
	hi, err2 := strconv.Atoi(strings.TrimSpace(b))
	return lo, hi, err1 == nil && err2 == nil && lo <= hi
}

// Anywhere is the source deploy opens its ports to; its preview names it.
const Anywhere = "0.0.0.0/0"

// Open adds an ingress rule from 0.0.0.0/0 to group sgID for each tcp and
// udp port that no rule opens yet. rec gets the group, the ports it needs,
// and the rules created here; rules that were already there are not
// recorded as its own. describe makes the rule description from a port
// label. Failures are reported, not returned: the VM works and the ports
// can be opened in the console.
func Open(ctx context.Context, cloud provision.CloudAPI, projectID, sgID string, tcp, udp []int, describe func(label string) string, rec *state.DeploymentRecord) OpenResult {
	var res OpenResult
	if sgID == "" {
		return res
	}
	rec.SecurityGroupID = sgID
	if len(tcp)+len(udp) == 0 {
		return res
	}

	// A port is open already when a rule lets it in from anywhere
	// (Coverage); one opened here is not opened a second time.
	before := map[string]bool{}
	rules, listErr := cloud.ListSecurityGroupRules(ctx, sgID)
	for _, r := range rules {
		before[r.ID] = true
	}
	openedHere := map[string]bool{}

	// errored: the create failed without a refusal (lost answer, 5xx): the
	// rule may exist, and is adopted if the listing shows it (QA r3 R6).
	type pending struct {
		key, desc string
		errored   bool
	}
	var lost []pending
	for _, set := range []struct {
		proto string
		ports []int
	}{{"tcp", tcp}, {"udp", udp}} {
		for _, p := range set.ports {
			key := PortKey(set.proto, p)
			addPort(rec, key)
			pr := fmt.Sprintf("%d:%d", p, p)
			label := Label(key)
			if covered, _ := Coverage(rules, set.proto, p, Anywhere); covered || openedHere[key] {
				res.AlreadyOpen = append(res.AlreadyOpen, label)
				if !ownsPort(rec, key) {
					addKey(&rec.PortsAlreadyOpen, key)
				}
				continue
			}
			openedHere[key] = true
			rule := provision.SGRule{
				Direction:      "ingress",
				EtherType:      "IPv4",
				IPProtocol:     set.proto,
				PortRange:      pr,
				RemoteIPPrefix: Anywhere,
				Description:    describe(label),
			}
			created, err := cloud.AddSecurityGroupRule(ctx, sgID, projectID, rule)
			if err != nil {
				if st := provision.HTTPStatus(err); st >= 400 && st < 500 {
					res.Failed = append(res.Failed, label) // refused: nothing was created
				} else {
					lost = append(lost, pending{key, rule.Description, true})
				}
				continue
			}
			res.Opened = append(res.Opened, label)
			if created != nil && created.ID != "" {
				addRule(rec, state.SGRule{ID: created.ID, SecurityGroupID: sgID, Port: key})
				dropKey(&rec.PortsAlreadyOpen, key)
			} else {
				lost = append(lost, pending{key, rule.Description, false})
			}
		}
	}

	// No ID in the create answer: find the new rule by what it is. Only
	// rules absent from the listing taken before count, and only when that
	// listing worked — otherwise an older rule could be taken for ours.
	if len(lost) > 0 {
		var after []*provision.SGRule
		if listErr == nil {
			after, _ = cloud.ListSecurityGroupRules(ctx, sgID)
		}
		for _, l := range lost {
			proto, port := splitKey(l.key)
			pr := fmt.Sprintf("%d:%d", port, port)
			found := ""
			for _, r := range after {
				if r.ID != "" && !before[r.ID] && r.Direction == "ingress" &&
					strings.EqualFold(r.IPProtocol, proto) && r.PortRange == pr && r.Description == l.desc {
					found = r.ID
					break
				}
			}
			if found == "" {
				if l.errored {
					res.Failed = append(res.Failed, Label(l.key))
				} else {
					res.Untracked = append(res.Untracked, Label(l.key))
				}
				continue
			}
			if l.errored {
				res.Opened = append(res.Opened, Label(l.key))
			}
			before[found] = true
			addRule(rec, state.SGRule{ID: found, SecurityGroupID: sgID, Port: l.key})
			dropKey(&rec.PortsAlreadyOpen, l.key)
		}
	}
	return res
}

func addPort(rec *state.DeploymentRecord, key string) { addKey(&rec.SecurityGroupPorts, key) }

func addKey(keys *[]string, key string) {
	if !slices.Contains(*keys, key) {
		*keys = append(*keys, key)
	}
}

func dropKey(keys *[]string, key string) {
	*keys = slices.DeleteFunc(*keys, func(k string) bool { return k == key })
	if len(*keys) == 0 {
		*keys = nil
	}
}

// ownsPort: a rule this deployment created opens port key.
func ownsPort(rec *state.DeploymentRecord, key string) bool {
	return slices.ContainsFunc(rec.OpenedRules, func(r state.SGRule) bool { return r.Port == key })
}

// KeyOf is the state key of a port label: "80" → "80/tcp".
func KeyOf(label string) string {
	if strings.Contains(label, "/") {
		return label
	}
	return label + "/tcp"
}

// NotOurs are the labels of AlreadyOpen that rec has no rule for: another
// rule opened them, and destroy leaves them.
func (r OpenResult) NotOurs(rec *state.DeploymentRecord) []string {
	var out []string
	for _, l := range r.AlreadyOpen {
		if !ownsPort(rec, KeyOf(l)) {
			out = append(out, l)
		}
	}
	return out
}

func addRule(rec *state.DeploymentRecord, r state.SGRule) {
	for _, have := range rec.OpenedRules {
		if have.ID == r.ID {
			return
		}
	}
	rec.OpenedRules = append(rec.OpenedRules, r)
}

// Note is a one-line summary of an OpenResult for MCP results.
func (r OpenResult) Note() string {
	var parts []string
	if len(r.Opened) > 0 {
		parts = append(parts, "opened ports "+strings.Join(r.Opened, ","))
	}
	if len(r.AlreadyOpen) > 0 {
		parts = append(parts, "already open "+strings.Join(r.AlreadyOpen, ","))
	}
	if len(r.Failed) > 0 {
		parts = append(parts, "FAILED to open "+strings.Join(r.Failed, ",")+" — open them manually in the console")
	}
	if len(r.Untracked) > 0 {
		parts = append(parts, "rule ID unknown for "+strings.Join(r.Untracked, ",")+" — destroy will not close it")
	}
	return strings.Join(parts, "; ")
}

// neededBy returns another live deployment in sf that needs port key in
// group sgID. A record written before security_group_id existed may be in
// any group: its tcp exposed_ports count.
func neededBy(sf *state.File, exceptID, sgID, key string) *state.DeploymentRecord {
	proto, port := splitKey(key)
	for i := range sf.Deployments {
		d := &sf.Deployments[i]
		if d.ID == exceptID || d.Status == "destroyed" || d.Status == "destroying" {
			continue
		}
		if d.SecurityGroupID == sgID {
			for _, k := range d.SecurityGroupPorts {
				if k == key {
					return d
				}
			}
		}
		if d.SecurityGroupID == "" && proto == "tcp" {
			for _, p := range d.ExposedPorts {
				if p == port {
					return d
				}
			}
		}
	}
	return nil
}

// Planned is one rule destroy will handle, for the preview.
type Planned struct {
	Rule     state.SGRule
	KeptFor  string // ID of the deployment that still needs it; empty: delete
	RecordID string // the deployment the rule belongs to
}

// Plan lists what Close would do with the rules of the given deployments.
func Plan(sf *state.File, recIDs ...string) []Planned {
	var out []Planned
	for _, id := range recIDs {
		rec := sf.FindDeployment(id)
		if rec == nil {
			continue
		}
		for _, r := range rec.OpenedRules {
			p := Planned{Rule: r, RecordID: id}
			if other := neededBy(sf, id, r.SecurityGroupID, r.Port); other != nil {
				p.KeptFor = other.ID
			}
			out = append(out, p)
		}
	}
	return out
}

// CloseResult says what Close did, by port label.
type CloseResult struct {
	Closed []string `json:"closed,omitempty"`
	Kept   []string `json:"kept,omitempty"`   // "80 (needed by deployment vm-2)"
	Failed []string `json:"failed,omitempty"` // "80: <error>"
}

// Empty reports whether there was nothing to do.
func (r CloseResult) Empty() bool { return len(r.Closed)+len(r.Kept)+len(r.Failed) == 0 }

// Add appends another result.
func (r *CloseResult) Add(o CloseResult) {
	r.Closed = append(r.Closed, o.Closed...)
	r.Kept = append(r.Kept, o.Kept...)
	r.Failed = append(r.Failed, o.Failed...)
}

// Close deletes the rules deployment recID created, in sf. A rule another
// deployment in sf still needs moves to that deployment (its destroy closes
// it). A rule whose delete fails stays on the record with DeleteError set;
// a rule already gone (404) counts as closed. Call it once the VM is
// deleted: closing first would cut a running app off if the VM delete
// then failed.
func Close(ctx context.Context, cloud provision.CloudAPI, sf *state.File, recID string) CloseResult {
	var res CloseResult
	rec := sf.FindDeployment(recID)
	if rec == nil || len(rec.OpenedRules) == 0 {
		return res
	}
	rules := rec.OpenedRules
	var left []state.SGRule
	for _, r := range rules {
		label := Label(r.Port)
		if other := neededBy(sf, recID, r.SecurityGroupID, r.Port); other != nil {
			r.DeleteError = ""
			addRule(other, r)
			res.Kept = append(res.Kept, fmt.Sprintf("%s (needed by deployment %s)", label, other.ID))
			continue
		}
		if r.ID == "" || r.SecurityGroupID == "" {
			// Only a hand-edited state has these: DELETE .../rules/ with an
			// empty ID is not this rule. Nothing to retry.
			res.Failed = append(res.Failed, label+": no rule or group ID in state — delete it in the Cloud.ru console")
			continue
		}
		err := cloud.DeleteSecurityGroupRule(ctx, r.SecurityGroupID, r.ID)
		var cliErr *clerrors.CLIError
		// 404 — the rule is gone: NOT_FOUND (VM_NOT_FOUND before QA-10).
		if err == nil || (stderrors.As(err, &cliErr) && (cliErr.Code == clerrors.CodeNotFound || cliErr.Code == clerrors.CodeVMNotFound)) {
			res.Closed = append(res.Closed, label)
			continue
		}
		r.DeleteError = err.Error()
		left = append(left, r)
		res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", label, err))
	}
	rec.OpenedRules = left
	rec.UpdatedAt = time.Now()
	return res
}

// Pending returns the IDs of deployments already destroyed whose rules are
// still to be deleted.
func Pending(sf *state.File) []string {
	var ids []string
	for _, d := range sf.Deployments {
		if d.Status == "destroyed" && len(d.OpenedRules) > 0 {
			ids = append(ids, d.ID)
		}
	}
	return ids
}

// Retire ends a destroyed deployment in sf: it is removed, unless rules it
// created are still to be deleted — then it stays as "destroyed" (not
// active, so deploy and status ignore it) for the next destroy to finish.
func Retire(sf *state.File, recID string) {
	rec := sf.FindDeployment(recID)
	if rec == nil {
		return
	}
	if len(rec.OpenedRules) == 0 {
		sf.RemoveDeployment(recID)
		return
	}
	rec.Status = "destroyed"
	rec.UpdatedAt = time.Now()
}
