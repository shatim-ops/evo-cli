// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
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
		return usageT("'path' is required (from secret_list)", "нужен 'path' (из secret_list)"), true
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
		return usageT("'path' is required (from secret_list)", "нужен 'path' (из secret_list)"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.T("secret_value returns the DECRYPTED secret in plaintext.",
				"secret_value возвращает РАСШИФРОВАННЫЙ секрет открытым текстом."),
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
		return usageT("'path' and 'value' are required", "нужны 'path' и 'value'"), true
	}
	// The secret's name is the last part of the path: "folder/" names none
	// (QA-15).
	if err := plainName("secret", "name", path[strings.LastIndex(path, "/")+1:]); err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("secret_create stores a new secret at %q. (The value is never echoed back.)",
				"secret_create сохраняет новый секрет по пути %q. (Значение в ответе не возвращается.)", path),
			// The path only: the value never goes into a preview.
			"preview": willPreview(engine.ResourceSecret, path, "", nil, nil),
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
	if err != nil || secretID(res) == "" {
		if err == nil {
			err = noID("secret")
		}
		// A secret is found by its path: the list's name of it.
		list := func(ctx context.Context) ([]map[string]any, error) {
			items, err := cli.List(ctx)
			for _, it := range items {
				it["name"] = it["path"]
			}
			return items, err
		}
		return lostCreate(ctx, list, i18n.T("secret", "секрет"), "secret_id", path, nil, "secret_get path", "secret_list", fmt.Errorf("create secret: %w", err))
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
		return usageT("'path' and 'value' are required", "нужны 'path' и 'value'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("secret_add_version adds a new value version to secret %q (the previous version stays until disabled).",
			"secret_add_version добавляет новую версию значения секрета %q (предыдущая действует, пока её не отключат).", path)}), true
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
		return usageT("'path' is required (from secret_list)", "нужен 'path' (из secret_list)"), true
	}
	cli, err := s.secretClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		// A secret that is not there is already deleted (D26).
		if _, err := cli.Get(ctx, path); err != nil {
			return s.deleteFailed("secret_delete", path, err)
		}
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("secret_delete PERMANENTLY deletes secret %q and all its versions.",
			"secret_delete БЕЗВОЗВРАТНО удаляет секрет %q и все его версии.", path)}), true
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
		return s.deleteFailed("secret_delete", path, fmt.Errorf("delete secret: %w", err))
	}
	return toJSON(map[string]any{"status": "deleted", "path": path}), false
}

// secretID is the id of the secret a create answered ({"secret": {...}}).
func secretID(res map[string]any) string {
	sec, _ := res["secret"].(map[string]any)
	id, _ := sec["id"].(string)
	return id
}
