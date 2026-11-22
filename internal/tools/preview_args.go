// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/stack"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// echoedArg is an optional argument a create sends as the call gives it
// and its preview names (Decl.EchoedArgs). summary, when set, is what the
// preview names for an argument whose text is too long for a line of the
// question — a PEM chain, a cloud-init template: what is in it and a
// SHA-256 of what is sent, so a person checks it and an agent sees what
// goes.
type echoedArg struct {
	name    string
	summary func(string) string
}

// certChainSummary names each certificate of a PEM chain (as given, or in
// Base64 as Certificate Manager takes it): subject, issuer, the last day
// and the SHA-256 of its DER; text that is no chain — its size and hash.
func certChainSummary(v string) string {
	text := strings.TrimLeft(v, " \t\r\n")
	if !strings.HasPrefix(text, "-----BEGIN ") {
		if b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v)); err == nil {
			text = string(b)
		}
	}
	var certs []string
	rest := []byte(text)
	for {
		var b *pem.Block
		if b, rest = pem.Decode(rest); b == nil {
			break
		}
		c, err := x509.ParseCertificate(b.Bytes)
		if b.Type != "CERTIFICATE" || err != nil {
			continue
		}
		certs = append(certs, i18n.Tf("subject %s, issuer %s, until %s, SHA-256 %x", "субъект %s, издатель %s, до %s, SHA-256 %x",
			quoteCloud(c.Subject.String()), quoteCloud(c.Issuer.String()), c.NotAfter.UTC().Format("2006-01-02"), sha256.Sum256(c.Raw)))
	}
	if len(certs) == 0 {
		return textSummary(v)
	}
	return i18n.Tf("%d certificate(s): %s", "сертификатов: %d; %s", len(certs), strings.Join(certs, "; "))
}

// templateSummary is the size and the lines of a template, with its first
// line where it is a header that says what the template is
// (templateHeader). Any other line is text the author wrote, and a template
// may carry a password or a token: the preview reaches a model and a log.
// No hash either: a short secret in an otherwise known template is found
// from it by a dictionary (risk conclusion 299).
func templateSummary(v string) string {
	s := sizeSummary(v)
	first, _, _ := strings.Cut(v, "\n")
	first = strings.TrimSuffix(first, "\r")
	switch {
	case templateHeaders[first]:
		s += i18n.Tf(", first line %s", ", первая строка %s", quoteCloud(first))
	case strings.HasPrefix(first, "#!"):
		// An interpreter outside the list: its path and arguments are the
		// author's text too (Астра, #2152).
		s += i18n.T(", first line: an interpreter (#!)", ", первая строка: интерпретатор (#!)")
	}
	return s
}

// templateHeaders are the first lines of a cloud-init template the preview
// names as they are: a header of cloud-init's own, the Jinja marker, or a
// common interpreter with no arguments. A closed list: any other line is
// the author's text.
var templateHeaders = map[string]bool{
	"#cloud-config": true, "#cloud-config-archive": true, "#cloud-boothook": true, "#include": true, "#part-handler": true,
	"## template: jinja": true,
	"#!/bin/bash":        true, "#!/bin/sh": true, "#!/usr/bin/env bash": true, "#!/usr/bin/env sh": true,
	"#!/usr/bin/python3": true, "#!/usr/bin/env python3": true,
}

// textSummary is the size, the lines and the SHA-256 of a text.
func textSummary(v string) string {
	return sizeSummary(v) + i18n.Tf(", SHA-256 %x", ", SHA-256 %x", sha256.Sum256([]byte(v)))
}

// sizeSummary is the size and the lines of a text.
func sizeSummary(v string) string {
	return i18n.Tf("%d bytes, %d lines", "%d байт, строк: %d", len(v), strings.Count(strings.TrimRight(v, "\n"), "\n")+1)
}

// namedArgs is the handler of a tool of Decl.EchoedArgs whose preview names
// the arguments of the list the call gives: in the params of the preview
// object where there is one (engine.CreatePreview), else beside the
// message, where the CLI shows every field. An argument the preview names
// already, under its own key, is left as it is. confirmation_required is
// the one status of a preview (gated): any other answer is not one.
func namedArgs(echoed []echoedArg, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		text, isErr := next(ctx, args)
		var m map[string]any
		if !isErr || json.Unmarshal([]byte(text), &m) != nil || m["status"] != "confirmation_required" {
			return text, isErr
		}
		p, isPreview := m["preview"].(map[string]any)
		params, _ := p["params"].([]any)
		named := map[string]bool{}
		for _, prm := range params {
			if pm, ok := prm.(map[string]any); ok {
				named[fmt.Sprint(pm["key"])] = true
			}
		}
		changed := false
		for _, a := range echoed {
			k := a.name
			v, ok := args[k]
			if !ok || v == nil || v == "" || named[k] {
				continue
			}
			if a.summary != nil {
				if text, isText := v.(string); isText {
					v = a.summary(text)
				}
			}
			if isPreview {
				if _, has := p[k]; has {
					continue
				}
				params = append(params, map[string]any{"key": k, "value": v})
			} else {
				if _, has := m[k]; has {
					continue
				}
				m[k] = v
			}
			changed = true
		}
		if !changed {
			return text, isErr
		}
		if isPreview {
			p["params"] = params
		}
		return toJSON(m), isErr
	}
}

func lbDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// approvalDigest keeps previously displayed offers compatible. It binds the exact
// POST body and resolved zone to the caller and the interrupted attempts shown.
func (p *lbPlacement) approvalDigest(interruptedIDs []string) (string, error) {
	if p.config.KeyID == "" && p.config.ServiceAccountID == "" {
		return "", usageErr("Cannot identify the caller for load balancer approval.", "Не удалось определить учётную запись для согласования балансировщика.")
	}
	terms, err := json.Marshal([]any{
		p.body, p.zoneID, p.config.Profile, p.config.ServiceAccountID,
		lbDigest([]byte(p.config.KeyID)), interruptedIDs,
	})
	if err != nil {
		return "", err
	}
	return lbDigest(terms), nil
}

// planLbCreate binds each way to execute the paid request to its approved placement.
// Both a direct call and a resumed stack open the intent only on that transition.
func (s *Registry) planLbCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	plan, placement, err := s.prepareLBCreate(ctx, args)
	if err != nil || plan.Execute == nil {
		return plan, err
	}
	preview := plan.Preview
	cuts := s.cutCalls(ctx, "lb_create", args)
	var ids []string
	if len(cuts) > 0 {
		message, warn := interruptedWarning(ctx, "lb_create", args, cuts)
		preview["interrupted"] = warn
		preview["message"] = message + " " + stringArg(preview, "message")
		for _, cut := range cuts {
			ids = append(ids, cut.ID)
		}
		slices.Sort(ids)
	}
	digest, err := placement.approvalDigest(ids)
	if err != nil {
		return nil, err
	}
	send := func(ctx context.Context) (string, bool) {
		if err := startPlacementIntent(ctx); err != nil {
			return errResult(err), true
		}
		return plan.Execute(ctx)
	}
	caller := callerOf(ctx)
	session, httpCall := ctx.Value(networkSessionKey{}).(string)
	scope := "stdio-" + s.upgradeClient
	if caller != nil {
		scope = "cli"
	} else if httpCall {
		if session == "" && ctx.Value(lbJobKey{}) == nil {
			return nil, clierrors.New(clierrors.CodeConfirmationRequired, i18n.T("An initialized MCP session is required.", "Нужна открытая сессия MCP."), i18n.T("Call initialize, send Mcp-Session-Id, then request a preview.", "Вызовите initialize, передайте Mcp-Session-Id и запросите превью."))
		}
		scope = "http-" + session
	}
	if job, _ := ctx.Value(lbJobKey{}).(*stack.Job); job != nil {
		root, ok := s.stateDir()
		if !ok {
			return nil, errNoStateDir()
		}
		jobPath := filepath.Join(root, "lb-previews", "stack-"+lbDigest([]byte(job.ID))+".json")
		if capture, _ := ctx.Value(lbPinKey{}).(bool); capture {
			unlock, err := state.FileLock(jobPath+".lock", 5*time.Second)
			if err != nil {
				return nil, err
			}
			defer unlock()
			pin, err := config.ReadFileNoFollow(jobPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			if len(pin) == 0 {
				if err := config.WriteDurableReceipt(jobPath, []byte(digest)); err != nil {
					return nil, err
				}
			}
			return &Plan{Preview: preview}, nil
		}
		if job.LBPending == nil {
			pin, err := config.ReadFileNoFollow(jobPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			if string(pin) == digest {
				return &Plan{Preview: preview, Execute: send}, nil
			}
		}
		scope = scope + ":stack:" + lbDigest([]byte(job.ID))
		if job.LBUncertain {
			warning := i18n.T("The preceding LB submission may already be billed. Inspect lb_list and the operation before explicitly approving a second creation.", "Предыдущее создание могло привести к оплате. Проверьте lb_list и операцию перед явным согласием на повторное создание.")
			preview["warning"] = warning
			human := preview["preview"].(engine.CreatePreview)
			human.Warnings = append(human.Warnings, warning)
			preview["preview"] = human
			digest = lbDigest([]byte("uncertain:" + digest))
		}
		if parent := ctx.Value(lbStackCLIKey{}); parent != nil {
			scope = "cli:stack:" + lbDigest([]byte(job.ID))
		}
		caller = nil // A paused stack always needs the actual offer returned to the operator.
		will, _ := preview["will_create"].(map[string]any)
		planned := job.LBPlacement
		if planned != nil && job.LBPending == nil && planned.Project == will["project_id"] && planned.VPC == will["vpc_id"] && planned.Zone == will["zone_id"] && planned.Subnet == will["subnet_id"] && planned.Name == will["name"] && planned.External == will["external"] && planned.Port == pgInt(args, "port", 0) && planned.TargetPort == pgInt(args, "target_port", pgInt(args, "port", 0)) {
			if _, cut := preview["interrupted"]; !cut {
				return &Plan{Preview: preview, Execute: send}, nil
			}
		}
	}
	if httpCall && session == "" {
		preview["status"] = "confirmation_required"
		preview["hint"] = i18n.T("Initialize an MCP session and request stack_apply for this job.", "Откройте сессию MCP и запросите stack_apply для этого задания.")
		return &Plan{Preview: preview, PreviewOf: func(context.Context) (map[string]any, error) { return preview, nil }, Execute: func(context.Context) (string, bool) { return toJSON(preview), true }}, nil
	}
	root, ok := s.stateDir()
	if !ok {
		return nil, errNoStateDir()
	}
	path := filepath.Join(root, "lb-previews", lbDigest([]byte(scope))+".json")
	type offer struct{ Digest, ID string }
	lock := func() (func(), error) { return state.FileLock(path+".lock", 5*time.Second) }
	read := func() (o offer, err error) {
		b, e := config.ReadFileNoFollow(path)
		if errors.Is(e, os.ErrNotExist) {
			return o, nil
		}
		if e != nil {
			return o, e
		}
		err = json.Unmarshal(b, &o)
		return
	}
	show := func() error {
		id, e := uuid.NewRandom()
		if e != nil {
			return e
		}
		b, e := json.Marshal(offer{digest, id.String()})
		if e != nil {
			return e
		}
		if e = config.WriteDurableReceipt(path, b); e != nil {
			return e
		}
		if caller == nil {
			preview["preview_id"] = id.String()
			preview["approval"] = i18n.T("Show this placement to the person. After explicit approval send its preview_id with confirmed=true in this session.", "Покажите размещение человеку. После явного согласия передайте preview_id с confirmed=true в этой сессии.")
		}
		return nil
	}
	return &Plan{Preview: preview, PreviewOf: func(context.Context) (map[string]any, error) {
		unlock, e := lock()
		if e != nil {
			return nil, e
		}
		defer unlock()
		if caller != nil {
			caller.lbDigest = digest
			if caller.AutomaticPreview {
				o, e := read()
				if e != nil {
					return nil, e
				}
				if o.Digest == "" || o.Digest == digest {
					return preview, nil
				}
				caller.RequireNewApproval = true
				return preview, nil
			}
		}
		return preview, show()
	}, Execute: func(ctx context.Context) (string, bool) {
		unlock, e := lock()
		if e != nil {
			return errResult(e), true
		}
		defer unlock()
		o, e := read()
		if e != nil {
			return errResult(e), true
		}
		valid := o.Digest == digest && o.ID == stringArg(args, "preview_id") && o.ID != ""
		if caller != nil {
			valid = caller.lbDigest == digest && (o.Digest == "" || o.Digest == digest)
		}
		if !valid {
			if e := show(); e != nil {
				return errResult(e), true
			}
			preview["status"] = "confirmation_required"
			return toJSON(preview), true
		}
		if o.ID != "" {
			if e := os.Remove(path); e != nil {
				return errResult(e), true
			}
		}
		return send(ctx)
	}}, nil
}

// planSnatCreate creates a SNAT gateway so a private subnet's nodes can reach
// the internet (pull public images, apt, outbound calls). Gated on confirmed.
func (s *Registry) planSnatCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	name := stringArg(args, "name")
	if name == "" {
		return answered(usageT("'name' is required", "нужен 'name'"), true), nil
	}
	if err := provision.ValidName(i18n.T("SNAT gateway", "SNAT-шлюза"), name); err != nil {
		return nil, err
	}
	placement, err := s.snatPlacement(ctx, args)
	if err != nil {
		return nil, err
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	plan, err := prepareSNATGateway(ctx, eng, name, placement.id, placement.name)
	if err != nil || plan.Execute == nil {
		return plan, err
	}
	return s.bindSNATPlacement(ctx, eng, plan, name, placement)
}

// bindSNATPlacement remembers the UUID displayed for a named gateway. A
// first confirmed call needs no offer. The CLI's implicit --yes planning
// pass must not overwrite an earlier offer before comparing the UUID.
func (s *Registry) bindSNATPlacement(ctx context.Context, eng *engine.Engine, plan *Plan, name string, placement snatPlacement) (*Plan, error) {
	root, ok := s.stateDir()
	if !ok {
		return nil, errNoStateDir()
	}
	execute := plan.Execute
	send := func(ctx context.Context) (string, bool) {
		if err := startPlacementIntent(ctx); err != nil {
			return errResult(err), true
		}
		return execute(ctx)
	}
	if placement.ref == "" {
		plan.Execute = send
		return plan, nil
	}
	caller := callerOf(ctx)
	// A retry can arrive on a new connection or after a process restart.
	// Share the offer only within the same credential, project and gateway.
	terms, _ := json.Marshal([]string{eng.Config.ProjectID, eng.Config.Profile, eng.Config.ServiceAccountID, eng.Config.KeyID, name})
	path := filepath.Join(root, "snat-previews", lbDigest(terms)+".json")
	type offer struct {
		ID      string `json:"id"`
		Changed bool   `json:"changed,omitempty"`
	}
	check := func(show bool) (bool, error) {
		unlock, err := state.FileLock(path+".lock", 5*time.Second)
		if err != nil {
			return false, err
		}
		defer unlock()
		var saved offer
		b, err := config.ReadFileNoFollow(path)
		if err == nil {
			if json.Unmarshal(b, &saved) != nil || !isUUID(saved.ID) {
				return false, clierrors.New(clierrors.CodeStateCorrupted, i18n.T("Cannot read the displayed SNAT placement.", "Не удалось прочитать сохранённое размещение SNAT."), "")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		changed := saved.Changed || (saved.ID != "" && saved.ID != placement.id)
		if caller != nil && !show && caller.snatZoneID != "" && caller.snatZoneID != placement.id {
			changed = true
		}
		if (show && saved.ID == "" && (caller == nil || !caller.AutomaticPreview)) || (changed && !saved.Changed) {
			if saved.ID == "" {
				saved.ID = placement.id
			}
			saved.Changed = changed
			b, _ := json.Marshal(saved)
			if err := config.WriteDurableReceipt(path, b); err != nil {
				return false, err
			}
		}
		if caller != nil {
			if show {
				caller.snatZoneID = placement.id
				caller.RequireNewApproval = changed && caller.AutomaticPreview
			} else if !caller.AutomaticPreview && caller.snatZoneID == placement.id {
				// The CLI has just asked the person about this exact UUID.
				return false, nil
			}
		}
		return changed, nil
	}
	markChanged := func() {
		message := i18n.T("The zone placement changed. Show the new UUID to the person. After fresh approval, pass will_create.availability_zone_id as availability_zone_id with confirmed=true.", "Размещение зоны изменилось. Покажите человеку новый UUID. После нового согласия передайте will_create.availability_zone_id как availability_zone_id вместе с confirmed=true.")
		if caller != nil {
			message = i18n.T("The zone placement changed. Review the new UUID and confirm it at the terminal, or pass the approved --availability-zone-id.", "Размещение зоны изменилось. Проверьте новый UUID и подтвердите его в терминале или передайте одобренный --availability-zone-id.")
		}
		plan.Preview["message"] = message
		human := plan.Preview["preview"].(engine.CreatePreview)
		human.Warnings = append(human.Warnings, message)
		plan.Preview["preview"] = human
	}
	return &Plan{Preview: plan.Preview, PreviewOf: func(context.Context) (map[string]any, error) {
		changed, err := check(true)
		if changed {
			markChanged()
		}
		return plan.Preview, err
	}, Execute: func(ctx context.Context) (string, bool) {
		changed, err := check(false)
		if err != nil {
			return errResult(err), true
		}
		if changed {
			markChanged()
			plan.Preview["status"] = "confirmation_required"
			return toJSON(plan.Preview), true
		}
		return send(ctx)
	}}, nil
}
