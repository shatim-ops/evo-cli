// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/certmanager"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// certClient builds an authenticated Certificate Manager client.
func (s *Registry) certClient(ctx context.Context) (*certmanager.Client, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return certmanager.New(tok.Value, eng.Config.ProjectID, ""), nil
}

func (s *Registry) handleCertList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	certs, err := cli.List(ctx, certmanager.ListOptions{
		Name:       stringArg(args, "name"),
		Type:       stringArg(args, "type"),
		DNSName:    stringArg(args, "dns_name"),
		CommonName: stringArg(args, "common_name"),
		State:      stringArg(args, "state"),
	})
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"project": cli.ProjectID(), "count": len(certs), "certificates": certs}), false
}

func (s *Registry) handleCertGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required", "нужен 'id'"), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	c, err := cli.Get(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(c), false
}

func (s *Registry) handleCertIssueLE(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	dnsNames := stringSliceArg(args, "dns_names")
	if name == "" || len(dnsNames) == 0 {
		return usageT("'name' and 'dns_names' (non-empty) are required", "нужны 'name' и непустой 'dns_names'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("cert_issue_le will request a Let's Encrypt certificate %q for %v (the domains must be delegated to Evolution DNS). Call again with confirmed=true after the user approves.",
				"cert_issue_le запросит сертификат Let's Encrypt %q для %v (домены должны быть делегированы в Evolution DNS). Получив согласие пользователя, вызовите снова с confirmed=true.", name, dnsNames),
		}), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.IssueLetsEncrypt(ctx, name, stringArg(args, "description"), dnsNames)
	if err != nil {
		return errResult(err), true
	}
	// Async: return the operation; the caller polls cert_operation until done.
	return toJSON(map[string]any{"status": "issuing", "note": "Let's Encrypt issuance is async — poll cert_operation with this operation id until done=true. Domains must have delegated zones in Evolution DNS.", "operation": op}), false
}

func (s *Registry) handleCertImport(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	server := stringArg(args, "server_certificate")
	key := stringArg(args, "private_key")
	if name == "" || server == "" || key == "" {
		return usageT("'name', 'server_certificate' (base64 PEM) and 'private_key' (base64 PEM) are required", "нужны 'name', 'server_certificate' (PEM в base64) и 'private_key' (PEM в base64)"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("cert_import will upload certificate %q and its private key to Certificate Manager. Call again with confirmed=true after the user approves.",
				"cert_import загрузит сертификат %q и его закрытый ключ в Certificate Manager. Получив согласие пользователя, вызовите снова с confirmed=true.", name),
		}), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{"name": name, "serverCertificate": server, "privateKey": key}
	if v := stringArg(args, "intermediate_certificate"); v != "" {
		body["intermediateCertificate"] = v
	}
	if v := stringArg(args, "description"); v != "" {
		body["description"] = v
	}
	c, err := cli.Import(ctx, body)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "imported", "certificate": c}), false
}

func (s *Registry) handleCertRenew(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "certificate_id")
	if id == "" {
		return usageT("'certificate_id' is required", "нужен 'certificate_id'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("cert_renew will trigger Let's Encrypt renewal of certificate %q. Call again with confirmed=true after the user approves.",
				"cert_renew запустит перевыпуск Let's Encrypt для сертификата %q. Получив согласие пользователя, вызовите снова с confirmed=true.", id),
		}), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.Renew(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "renewing", "operation": op}), false
}

func (s *Registry) handleCertDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required", "нужен 'id'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("cert_delete permanently deletes certificate %q. If it is bound to a load balancer/listener, deletion can break HTTPS. Get the user's approval, then call again with confirmed=true (add force=true to delete despite active usage).",
				"cert_delete безвозвратно удаляет сертификат %q. Если он привязан к балансировщику или обработчику, удаление может сломать HTTPS. Получите согласие пользователя, затем вызовите снова с confirmed=true (force=true — удалить, хотя сертификат используется).", id),
		}), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	force, _ := args["force"].(bool)
	if err := cli.Delete(ctx, id, force); err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "deleted", "id": id, "force": force}), false
}

func (s *Registry) handleCertUsage(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required", "нужен 'id'"), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	u, err := cli.Usage(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(u), false
}

func (s *Registry) handleCertVersions(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "certificate_id")
	if id == "" {
		return usageT("'certificate_id' is required", "нужен 'certificate_id'"), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	v, err := cli.Versions(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(v), false
}

func (s *Registry) handleCertPrivateKey(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "certificate_id")
	if id == "" {
		return usageT("'certificate_id' is required", "нужен 'certificate_id'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.T("cert_private_key returns the SECRET private key in plaintext. Only fetch it with the user's explicit approval; call again with confirmed=true.",
				"cert_private_key возвращает СЕКРЕТНЫЙ закрытый ключ открытым текстом. Запрашивайте его только с явного согласия пользователя; вызовите снова с confirmed=true."),
		}), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	ver := int64(pgInt(args, "version_id", 0))
	pk, err := cli.PrivateKey(ctx, id, ver)
	if err != nil {
		return errResult(err), true
	}
	pemKey(pk)
	return toJSON(pk), false
}

// pemKey turns the private key of a GetPrivateKey answer into PEM text, as
// the description promises: the API sends the PEM Base64-encoded (swagger:
// «Приватный ключ PEM в декодированном Base64»), and the tool passed that
// on (release audit, L-19). The swagger names the field privateKey, the
// live API sends private_key (live run 18.10: the key came out as Base64,
// key_format missing); the field the API sent is decoded in place.
// key_format says what it holds: "pem", or "base64" when the value does
// not decode to a PEM block (passed on as the API sent it). Over HTTP the
// redaction of both names still applies.
func pemKey(pk map[string]any) {
	field := "privateKey"
	v, _ := pk[field].(string)
	if v == "" {
		field = "private_key"
		v, _ = pk[field].(string)
	}
	if v == "" {
		return
	}
	if strings.HasPrefix(strings.TrimSpace(v), "-----BEGIN") {
		pk["key_format"] = "pem"
		return
	}
	if b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v)); err == nil && strings.HasPrefix(strings.TrimSpace(string(b)), "-----BEGIN") {
		pk[field] = string(b)
		pk["key_format"] = "pem"
		return
	}
	pk["key_format"] = "base64"
}

func (s *Registry) handleCertOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageT("'operation_id' is required", "нужен 'operation_id'"), true
	}
	cli, err := s.certClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if w := pgInt(args, "wait_seconds", 0); w > 0 {
		m, err := waitForOpJSON(ctx, w, func(ctx context.Context) (map[string]any, error) {
			op, err := cli.GetOperation(ctx, id)
			if err != nil {
				return nil, err
			}
			return opToMap(op), nil
		})
		if err != nil {
			return errResult(err), true
		}
		return toJSON(m), false
	}
	op, err := cli.GetOperation(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(op), false
}
