// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/shatim-ops/evo-cli/internal/secret"
)

// secretClient builds an authenticated Secret Management client.
func (s *Registry) secretClient(ctx context.Context) (*secret.Client, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return secret.New(tok.Value, eng.Config.ProjectID, ""), nil
}

func (s *Registry) handleSecretList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.secretClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.List(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{"id": m["id"], "name": m["name"], "path": m["path"], "description": m["description"]})
	}
	return toJSON(map[string]any{"secrets": out}), false
}

func (s *Registry) handleSecretGet(ctx context.Context, args map[string]any) (string, bool) {
	path := stringArg(args, "path")
	if path == "" {
		return usageResult(fmt.Errorf("'path' is required (from secret_list)")), true
	}
	cli, err := s.secretClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.Get(ctx, path)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// handleSecretValue returns the DECRYPTED secret value. Sensitive: the value is
// returned to the caller but must never be logged.
func (s *Registry) handleSecretValue(ctx context.Context, args map[string]any) (string, bool) {
	path := stringArg(args, "path")
	if path == "" {
		return usageResult(fmt.Errorf("'path' is required (from secret_list)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "secret_value returns the DECRYPTED secret in plaintext. Only fetch it with the user's explicit approval; call again with confirmed=true.",
		}), true
	}
	cli, err := s.secretClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	payload, err := cli.AccessPayload(ctx, path, pgInt(args, "version", 0))
	if err != nil {
		return errResult(err), true
	}
	dec, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// Not base64 — return as-is.
		return toJSON(map[string]any{"path": path, "value": payload}), false
	}
	return toJSON(map[string]any{"path": path, "value": string(dec)}), false
}

func (s *Registry) handleSecretCreate(ctx context.Context, args map[string]any) (string, bool) {
	path := stringArg(args, "path")
	value := stringArg(args, "value")
	if path == "" || value == "" {
		return usageResult(fmt.Errorf("'path' and 'value' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("secret_create stores a new secret at %q. Call again with confirmed=true. (The value is never echoed back.)", path),
		}), true
	}
	cli, err := s.secretClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{
		"projectId": cli.ProjectID(),
		"path":      path,
		"payload":   base64.StdEncoding.EncodeToString([]byte(value)),
	}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	if k := stringArg(args, "kms_key_id"); k != "" {
		body["kmsKeyId"] = k
	}
	res, err := cli.Create(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create secret: %w", err)), true
	}
	// Strip any payload echoed back before returning.
	if sec, ok := res["secret"].(map[string]any); ok {
		delete(sec, "payload")
	}
	return toJSON(map[string]any{"status": "created", "secret": res["secret"]}), false
}

func (s *Registry) handleSecretAddVersion(ctx context.Context, args map[string]any) (string, bool) {
	path := stringArg(args, "path")
	value := stringArg(args, "value")
	if path == "" || value == "" {
		return usageResult(fmt.Errorf("'path' and 'value' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("secret_add_version adds a new value version to secret %q (the previous version stays until disabled). Call again with confirmed=true.", path)}), true
	}
	cli, err := s.secretClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{
		"projectId": cli.ProjectID(),
		"path":      path,
		"payload":   base64.StdEncoding.EncodeToString([]byte(value)),
	}
	res, err := cli.AddVersion(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("add version: %w", err)), true
	}
	return toJSON(map[string]any{"status": "version_added", "version": res["version"]}), false
}

func (s *Registry) handleSecretDelete(ctx context.Context, args map[string]any) (string, bool) {
	path := stringArg(args, "path")
	if path == "" {
		return usageResult(fmt.Errorf("'path' is required (from secret_list)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("secret_delete PERMANENTLY deletes secret %q and all its versions. Call again with confirmed=true.", path)}), true
	}
	cli, err := s.secretClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	// confirmed=true already authorizes deleting the secret AND all its versions
	// (see the confirmation message), so force defaults to true — otherwise the
	// API 400s with "cannot delete the secret with active versions" and the agent
	// has to discover the force flag by trial (it did, during a live teardown).
	// An explicit force:false is still honored for a versions-must-be-empty delete.
	force := true
	if v, ok := args["force"].(bool); ok {
		force = v
	}
	if err := cli.Delete(ctx, path, force); err != nil {
		return errResult(fmt.Errorf("delete secret: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleted", "path": path}), false
}
