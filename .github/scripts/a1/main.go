// SPDX-License-Identifier: Apache-2.0
// A one-group check of the candidate's real rules-only destroy.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

const compute = "https://compute.api.cloud.ru"
const groups = "/api/v1/security-groups"
const zoneName = "ru.AZ-2"
const port = "18085"
const cidr = "198.18.255.1/32"

var stop = errors.New("scope or outcome unconfirmed")
var uuid = regexp.MustCompile("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")
var number = regexp.MustCompile("^[1-9][0-9]{0,19}$")

func id(s string) bool { return uuid.MatchString(s) && s != "00000000-0000-0000-0000-000000000000" }

type receipt struct {
	Run, Project, Principal, ZoneID, Group, Rule                          string
	GroupPOST, RulePOST, RuleDELETE, GroupDELETE, RuleDeleteAccepted      bool
	CandidateConfirmed, RuleGone, GroupGone                               bool
	InitialRulesUnclear, InitialEmpty, ScopeMismatch, GroupDeleteAccepted bool
}

type job struct {
	r                receipt
	dir, key, bearer string
	raw              *http.Client
	pause            time.Duration
	cardCounts       counts
}

func (j *job) name() string { return "evo-a1-" + j.r.Run }
func (j *job) path() string { return filepath.Join(j.dir, "receipt.json") }
func (j *job) save() error {
	f, err := os.CreateTemp(j.dir, ".receipt-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(j.r)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), j.path()); err != nil {
		return err
	}
	dir, err := os.Open(j.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (j *job) mismatch() error {
	j.r.ScopeMismatch = true
	if err := j.save(); err != nil {
		return err
	}
	return stop
}

func (j *job) loadReceipt(b []byte) error {
	var r receipt
	if json.Unmarshal(b, &r) != nil {
		return stop
	}
	// Only our writer's complete, canonical receipt is admissible. The
	// destination must start empty: {} and null must never inherit a scope.
	canonical, err := json.Marshal(r)
	if err != nil || !bytes.Equal(bytes.TrimSpace(b), canonical) || r.Run != j.r.Run || r.Principal != j.r.Principal || r.Project != j.r.Project {
		return stop
	}
	if !r.GroupPOST || !id(r.ZoneID) || r.Group != "" && !id(r.Group) || r.Rule != "" && !id(r.Rule) {
		return stop
	}
	if r.InitialEmpty && (!id(r.Group) || r.InitialRulesUnclear) || r.RulePOST && !r.InitialEmpty || r.Rule != "" && !r.RulePOST {
		return stop
	}
	if r.RuleDELETE && !id(r.Rule) || r.RuleDeleteAccepted && !r.RuleDELETE || r.RuleGone && !r.RuleDeleteAccepted || r.CandidateConfirmed && !r.RuleGone {
		return stop
	}
	if r.GroupDELETE && (!r.InitialEmpty || r.RulePOST && !r.RuleGone) || r.GroupDeleteAccepted && !r.GroupDELETE || r.GroupGone && !r.GroupDeleteAccepted {
		return stop
	}
	j.r = r
	return nil
}

func (j *job) call(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(b)
	}
	r, err := http.NewRequestWithContext(ctx, method, compute+path, reader)
	if err != nil {
		return 0, nil, err
	}
	r.Header.Set("Authorization", "Bearer "+j.bearer)
	r.Header.Set("Content-Type", "application/json")
	resp, err := j.raw.Do(r)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPartialContent || resp.Header.Get("Content-Range") != "" {
		return 0, nil, stop
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if len(b) > 1<<20 || resp.ContentLength > 0 && int64(len(b)) != resp.ContentLength {
		return 0, nil, stop
	}
	return resp.StatusCode, b, err
}

// The create answer and exact group card may expose directional counts
// before a new rule becomes visible in the group's paged rule list.
type counts struct {
	In  json.RawMessage `json:"ingress_rules_count"`
	Out json.RawMessage `json:"egress_rules_count"`
}

func countMatches(raw json.RawMessage, want int) bool {
	if raw == nil {
		return true
	}
	var n *int
	return json.Unmarshal(raw, &n) == nil && n != nil && *n == want
}
func (c counts) matches(in, out int) bool { return countMatches(c.In, in) && countMatches(c.Out, out) }

func (j *job) postID(ctx context.Context, path string, body any) (string, error) {
	code, b, err := j.call(ctx, http.MethodPost, path, body)
	var response struct {
		ID string
		counts
	}
	if err != nil || code < 200 || code >= 300 || json.Unmarshal(b, &response) != nil || !id(response.ID) {
		return "", stop
	}
	if path == groups {
		j.r.InitialRulesUnclear = !response.matches(0, 0)
	}
	return response.ID, nil
}

func (j *job) pauseFor(ctx context.Context) error {
	timer := time.NewTimer(j.pause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Use the candidate's two recognised Compute messages for this exact GET.
// Generic not-found text, route failures and inconsistent service codes stop.
func groupAbsent(b []byte) bool {
	type refusal struct {
		Code    json.RawMessage
		Message string
	}
	var entries []refusal
	if json.Unmarshal(b, &entries) != nil {
		var entry refusal
		if json.Unmarshal(b, &entry) != nil {
			return false
		}
		entries = []refusal{entry}
	}
	if len(entries) != 1 {
		return false
	}
	e := entries[0]
	if e.Code != nil {
		var code string
		if json.Unmarshal(e.Code, &code) != nil || code != "security_group_not_found" {
			return false
		}
	}
	switch strings.ToLower(strings.TrimSpace(e.Message)) {
	case "security group not found", "security group with this id is not found":
		return true
	}
	return false
}

func (j *job) group(ctx context.Context) (gone bool, err error) {
	if !id(j.r.Group) {
		return false, stop
	}
	for {
		code, b, err := j.call(ctx, http.MethodGet, groups+"/"+j.r.Group, nil)
		if err != nil {
			return false, err
		}
		if code == 404 && groupAbsent(b) {
			return true, nil
		}
		var c struct {
			counts
			ID, Name, Description, State string
			HasInterfaces                *bool                `json:"has_interfaces"`
			Default                      bool                 `json:"default"`
			ZoneDetail                   *provision.ZoneRef   `json:"availability_zone"`
			ProjectDetail                *struct{ ID string } `json:"project"`
			Project                      string               `json:"project_id"`
			Zone                         string               `json:"availability_zone_id"`
		}
		if code != 200 || json.Unmarshal(b, &c) != nil {
			return false, stop
		}
		if c.ID != j.r.Group || c.Name != j.name() || c.Description != j.name() || c.HasInterfaces == nil || *c.HasInterfaces || c.Default {
			return false, j.mismatch()
		}
		if c.Project != "" && c.Project != j.r.Project || c.ProjectDetail != nil && c.ProjectDetail.ID != j.r.Project || c.Zone != "" && c.Zone != j.r.ZoneID {
			return false, j.mismatch()
		}
		if z := c.ZoneDetail; z != nil && (z.ID != j.r.ZoneID || z.Name != "" && z.Name != zoneName) {
			return false, j.mismatch()
		}
		switch c.State {
		case "created", "available", "ready", "running":
			j.cardCounts = c.counts
			return false, nil
		case "creating", "updating", "deleting":
			if err := j.pauseFor(ctx); err != nil {
				return false, err
			}
		default:
			return false, stop
		}
	}
}

func (j *job) expectedRule() provision.SGRule {
	return provision.SGRule{Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: port + ":" + port, RemoteIPPrefix: cidr, Description: j.name()}
}

// A fresh empty group with one test rule always fits on its first page.
// An incomplete, ambiguous, unrecognised or unexpectedly larger list stops.
func (j *job) contents(ctx context.Context) (present bool, err error) {
	if j.r.ScopeMismatch {
		return false, stop
	}
	gone, err := j.group(ctx)
	if err != nil || gone {
		return false, stop
	}
	code, b, err := j.call(ctx, http.MethodGet, groups+"/"+j.r.Group+"/rules?offset=0&limit=100", nil)
	if err != nil || code != http.StatusOK {
		return false, stop
	}
	return j.inspectRulePage(b, j.cardCounts)
}

// Validate the actual response of every rule-list GET, including GETs
// issued by the candidate. A foreign row must survive in the receipt even
// if an eventually consistent list no longer returns it on the next read.
func (j *job) inspectRulePage(b []byte, observedCounts counts) (bool, error) {
	var page struct {
		Items  *[]*provision.SGRule `json:"items"`
		Total  *int                 `json:"total"`
		Offset *int                 `json:"offset"`
		Limit  *int                 `json:"limit"`
	}
	if json.Unmarshal(b, &page) != nil || page.Items == nil || page.Total == nil || *page.Total != len(*page.Items) || page.Offset != nil && *page.Offset != 0 || page.Limit != nil && (*page.Limit <= 0 || *page.Limit < len(*page.Items) || *page.Limit > 100) {
		return false, j.mismatch()
	}
	var meta map[string]json.RawMessage
	if json.Unmarshal(b, &meta) != nil {
		return false, j.mismatch()
	}
	for _, field := range []string{"next", "next_page", "next_offset", "next_page_token", "nextPageToken", "has_next", "has_more", "hasNext"} {
		if raw, ok := meta[field]; ok {
			switch string(bytes.TrimSpace(raw)) {
			case "null", `""`, "false":
			default:
				return false, j.mismatch()
			}
		}
	}
	rows := *page.Items
	if len(rows) == 0 {
		if !observedCounts.matches(0, 0) {
			return false, j.mismatch()
		}
		return false, nil
	}
	if len(rows) != 1 || rows[0] == nil || !id(j.r.Rule) || !observedCounts.matches(1, 0) {
		return false, j.mismatch()
	}
	r := rows[0]
	want := j.expectedRule()
	if r.ID != j.r.Rule || r.Direction != want.Direction || r.EtherType != want.EtherType || r.IPProtocol != want.IPProtocol || r.PortRange != want.PortRange || r.RemoteIPPrefix != want.RemoteIPPrefix || r.Description != want.Description || r.RemoteSecurityGroup != nil || r.RemoteSecurityGroupID != "" {
		return false, j.mismatch()
	}
	return true, nil
}

// The candidate adapter can read only this group's rules. Its one DELETE is
// allowed only for the direct POST IDs, after an independent complete read.
type fence struct{ j *job }

func acceptedDelete(code int) bool {
	return code == http.StatusOK || code == http.StatusAccepted || code == http.StatusNoContent
}

func (f fence) RoundTrip(r *http.Request) (*http.Response, error) {
	j := f.j
	rules := groups + "/" + j.r.Group + "/rules"
	if j.r.ScopeMismatch || !id(j.r.Group) || r.URL.Scheme != "https" || r.URL.Host != "compute.api.cloud.ru" || r.URL.User != nil || r.URL.Opaque != "" {
		return nil, stop
	}
	switch {
	case r.Method == http.MethodGet && r.URL.EscapedPath() == rules && r.URL.Query().Get("offset") == "0" && r.URL.Query().Get("limit") == "100" && len(r.URL.Query()) == 2:
	case r.Method == http.MethodDelete && id(j.r.Rule) && r.URL.EscapedPath() == rules+"/"+j.r.Rule && r.URL.RawQuery == "" && r.Body == nil && !j.r.RuleDELETE:
		present, err := j.contents(r.Context())
		if err != nil || !present {
			return nil, stop
		}
		j.r.RuleDELETE = true
		if err := j.save(); err != nil {
			return nil, err
		}
	default:
		return nil, stop
	}
	resp, err := j.raw.Transport.RoundTrip(r)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode == http.StatusPartialContent || resp.Header.Get("Content-Range") != "" {
		resp.Body.Close()
		return nil, j.mismatch()
	}
	if r.Method == http.MethodGet && resp.StatusCode != http.StatusOK || r.Method == http.MethodDelete && !acceptedDelete(resp.StatusCode) {
		return resp, err
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	resp.Body.Close()
	if err != nil || len(b) > 1<<20 || resp.ContentLength > 0 && int64(len(b)) != resp.ContentLength {
		return nil, j.mismatch()
	}
	if r.Method == http.MethodDelete {
		j.r.RuleDeleteAccepted = true
		if err = j.save(); err != nil {
			return nil, err
		}
	} else if _, err = j.inspectRulePage(b, counts{}); err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	return resp, nil
}

type token string

func (v token) Token(context.Context) (auth.Token, error) { return auth.Token{Value: string(v)}, nil }

func (j *job) destroyRule(ctx context.Context) error {
	eng := engine.Engine{Config: config.Config{ProjectID: j.r.Project, KeyID: j.key}, Auth: token(j.bearer), StateDir: j.dir,
		Printer: output.NewWithWriters(io.Discard, io.Discard, output.ModeJSON, false)}
	adapter := provision.NewCloudRuAdapter(&http.Client{Transport: fence{j}, Timeout: 15 * time.Second, CheckRedirect: noRedirect}, compute)
	eng.Cloud = adapter
	rec := state.DeploymentRecord{ID: j.name(), Status: "destroyed", ProjectID: j.r.Project, CreatedBy: eng.Maker(),
		OpenedRules: []state.SGRule{{ID: j.r.Rule, SecurityGroupID: j.r.Group, Port: port + "/tcp"}}}
	path := state.DefaultPath(j.dir)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if j.r.RuleDELETE {
			return stop
		}
		if err := state.Write(path, &state.File{Version: state.CurrentVersion, Deployments: []state.DeploymentRecord{rec}}); err != nil {
			return err
		}
	} else {
		f, err := state.Read(path)
		if err != nil || len(f.Deployments) != 1 {
			return stop
		}
		d := &f.Deployments[0]
		same, known := d.CreatedBy.MatchRuleOwner(eng.Maker())
		if !same || !known || d.ID != rec.ID || d.ProjectID != rec.ProjectID || d.Status != "destroyed" || d.VMID != "" || len(d.OpenedRules) != 1 ||
			d.OpenedRules[0].ID != j.r.Rule || d.OpenedRules[0].SecurityGroupID != j.r.Group {
			return stop
		}
	}
	var result engine.DestroyResult
	if err := eng.Destroy(ctx, engine.DestroyOptions{Force: true, Result: &result}); err != nil {
		return err
	}
	if !j.r.RuleDeleteAccepted || result.CleanupPending {
		return stop
	}
	f, err := state.Read(state.DefaultPath(j.dir))
	if err != nil || f.FindDeployment(j.name()) != nil {
		return stop
	}
	present, err := j.contents(ctx)
	if err != nil || present {
		return stop
	}
	j.r.CandidateConfirmed, j.r.RuleGone = true, true
	return j.save()
}

func (j *job) probe(ctx context.Context) error {
	if j.r.GroupPOST || !id(j.r.Project) {
		return stop
	}
	zoneAPI := provision.NewCloudRuAdapter(j.raw, compute)
	zoneAPI.SetToken(j.bearer)
	zones, err := zoneAPI.ListAvailabilityZones(ctx)
	if err != nil {
		return err
	}
	for _, z := range zones {
		if z == nil {
			return stop
		}
		if z.Name == zoneName {
			if !id(z.ID) || j.r.ZoneID != "" || z.Enabled != nil && !*z.Enabled {
				return stop
			}
			j.r.ZoneID = z.ID
		}
	}
	if j.r.ZoneID == "" {
		return stop
	}
	j.r.GroupPOST = true
	if err := j.save(); err != nil {
		return err
	}
	g, err := j.postID(ctx, groups, map[string]string{"project_id": j.r.Project, "availability_zone_id": j.r.ZoneID, "name": j.name(), "description": j.name()})
	if err != nil {
		return err
	}
	j.r.Group = g
	if err = j.save(); err != nil {
		return err
	}
	if j.r.InitialRulesUnclear {
		return stop
	}
	if present, err := j.contents(ctx); err != nil || present {
		return stop
	}
	j.r.InitialEmpty = true
	j.r.RulePOST = true
	if err = j.save(); err != nil {
		return err
	}
	r, err := j.postID(ctx, groups+"/"+g+"/rules?project_id="+j.r.Project, j.expectedRule())
	if err != nil {
		return err
	}
	j.r.Rule = r
	if err = j.save(); err != nil {
		return err
	}
	for {
		present, err := j.contents(ctx)
		if err != nil {
			return err
		}
		if present {
			break
		}
		if err = j.pauseFor(ctx); err != nil {
			return err
		}
	}
	err = j.destroyRule(ctx)
	if err == nil || !j.r.RuleDeleteAccepted {
		return err
	}
	// A successful DELETE may leave the rule in "deleting". Wait by ID, then
	// give the candidate the exact partial state it saved to finish the retry.
	for {
		present, err := j.contents(ctx)
		if err != nil {
			return err
		}
		if !present {
			break
		}
		if err := j.pauseFor(ctx); err != nil {
			return err
		}
	}
	return j.destroyRule(ctx)
}

func (j *job) cleanup(ctx context.Context) error {
	if !j.r.GroupPOST {
		return nil
	}
	if j.r.RuleDELETE && !j.r.RuleDeleteAccepted {
		return stop
	}
	if !id(j.r.Group) || !j.r.InitialEmpty || j.r.ScopeMismatch || j.r.InitialRulesUnclear || j.r.RulePOST && !id(j.r.Rule) {
		return stop
	}
	if !j.r.GroupDELETE {
		present, err := j.contents(ctx)
		if err != nil {
			return err
		}
		if present && !j.r.RuleDELETE {
			// This may finish the candidate path after a failed probe, from the
			// same receipt. No fallback direct rule DELETE is available.
			if err := j.destroyRule(ctx); err != nil && !j.r.RuleDeleteAccepted {
				return err
			}
		}
		for {
			present, err = j.contents(ctx)
			if err != nil {
				return err
			}
			if !present {
				break
			}
			if !j.r.RuleDeleteAccepted {
				return stop
			}
			if err := j.pauseFor(ctx); err != nil {
				return err
			}
		}
		if j.r.RulePOST {
			if !j.r.RuleDeleteAccepted {
				return stop
			}
			j.r.RuleGone = true
		}
		// No conditional group deletion is documented. The operator's review
		// before dispatch covers the interval between this read and DELETE.
		j.r.GroupDELETE = true
		if err := j.save(); err != nil {
			return err
		}
		code, _, err := j.call(ctx, http.MethodDelete, groups+"/"+j.r.Group, nil)
		if err != nil || !acceptedDelete(code) {
			return stop
		}
		j.r.GroupDeleteAccepted = true
		if err := j.save(); err != nil {
			return err
		}
	}
	if !j.r.GroupDeleteAccepted {
		return stop
	}
	for {
		gone, err := j.group(ctx)
		if err != nil {
			return err
		}
		if gone {
			j.r.GroupGone = true
			return j.save()
		}
		if err := j.pauseFor(ctx); err != nil {
			return err
		}
	}
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
func execute(mode string) error {
	if mode != "probe" && mode != "cleanup" {
		return stop
	}
	run, project := os.Getenv("GITHUB_RUN_ID"), os.Getenv("CLOUDRU_PROJECT_ID")
	if !number.MatchString(run) || os.Getenv("GITHUB_RUN_ATTEMPT") != "1" || !id(project) {
		return stop
	}
	tmp := os.Getenv("RUNNER_TEMP")
	if !filepath.IsAbs(tmp) {
		return stop
	}
	dir := filepath.Join(tmp, "a1-private")
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return stop
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return stop
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return stop
	}
	key := os.Getenv("CLOUDRU_KEY_ID")
	principal := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	j := job{r: receipt{Run: run, Project: project, Principal: principal}, dir: dir, key: key, pause: 2 * time.Second}
	receiptUnknown := mode == "cleanup"
	defer func() {
		fmt.Printf("candidate_confirmed=%t rule_deleted=%t group_deleted=%t possible_residue=%t\n", j.r.CandidateConfirmed, j.r.RuleGone, j.r.GroupGone, receiptUnknown || j.r.GroupPOST && !j.r.GroupGone)
	}()
	if mode == "cleanup" {
		info, err := os.Lstat(j.path())
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 8192 {
			fmt.Println("receipt_unavailable")
			return stop
		}
		b, err := os.ReadFile(j.path())
		if err != nil || j.loadReceipt(b) != nil {
			fmt.Println("receipt_unavailable")
			return stop
		}
		receiptUnknown = false
	} else if _, err := os.Lstat(j.path()); !errors.Is(err, os.ErrNotExist) {
		return stop
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	transport.DisableKeepAlives = true
	defer transport.CloseIdleConnections()
	j.raw = &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: noRedirect}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	secret := os.Getenv("CLOUDRU_SECRET")
	if j.key == "" || secret == "" {
		return stop
	}
	t, err := auth.NewIAMProvider(auth.Credentials{KeyID: j.key, Secret: secret}, j.raw, nil).Token(ctx)
	if err != nil {
		return stop
	}
	j.bearer = t.Value
	if mode == "cleanup" {
		return j.cleanup(ctx)
	}
	return j.probe(ctx)
}

func main() {
	if len(os.Args) != 2 || execute(os.Args[1]) != nil {
		os.Exit(1)
	}
}
