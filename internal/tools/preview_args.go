// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// echoedArgs are the optional arguments a create sends as the call gives
// them and its preview names as they are (CLAUDE.md §6, axis 4: the
// preview names everything evo sends; release plan 1.6, «Превью называет
// заданные необязательные аргументы»). One point for them all, as
// namedProject is for the project, not a line in each handler. A secret
// never goes here: the preview shows the value as the call gives it, or
// its summary (summarizedArgs).
var echoedArgs = map[string][]string{
	"cert_issue_le":    {"description"},
	"cert_import":      {"description", "intermediate_certificate"},
	"dns_zone_create":  {"description"},
	"image_create":     {"description", "display_name", "user_data_template"},
	"kafka_create":     {"description"},
	"lb_create":        {"target_port"},
	"pg_create":        {"description"},
	"pg_db_create":     {"lc_collate", "lc_ctype"},
	"redis_create":     {"description"},
	"vpc_create":       {"description"},
	"vpc_route_create": {"description"},
}

// summarizedArgs are the arguments of echoedArgs whose text is too long
// for a line of the question — a PEM chain, a cloud-init template: the
// preview names what is in it and a SHA-256 of what is sent, so a person
// checks it and an agent sees what goes.
var summarizedArgs = map[string]func(string) string{
	"intermediate_certificate": certChainSummary,
	"user_data_template":       templateSummary,
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

// templateSummary is the size, the lines and the SHA-256 of a template,
// with its first line where it says what it is (#cloud-config, #!).
func templateSummary(v string) string {
	s := textSummary(v)
	if first, _, _ := strings.Cut(v, "\n"); strings.HasPrefix(first, "#") {
		s += i18n.Tf(", first line %s", ", первая строка %s", quoteCloud(strings.TrimRight(first, "\r")))
	}
	return s
}

// textSummary is the size, the lines and the SHA-256 of a text.
func textSummary(v string) string {
	return i18n.Tf("%d bytes, %d lines, SHA-256 %x", "%d байт, строк: %d, SHA-256 %x",
		len(v), strings.Count(strings.TrimRight(v, "\n"), "\n")+1, sha256.Sum256([]byte(v)))
}

// namedArgs is the handler of a tool of echoedArgs whose preview names
// the arguments of the list the call gives: in the params of the preview
// object where there is one (engine.CreatePreview), else beside the
// message, where the CLI shows every field. An argument the preview names
// already, under its own key, is left as it is. confirmation_required is
// the one status of a preview (gated): any other answer is not one.
func namedArgs(keys []string, next Handler) Handler {
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
		for _, k := range keys {
			v, ok := args[k]
			if !ok || v == nil || v == "" || named[k] {
				continue
			}
			if sum := summarizedArgs[k]; sum != nil {
				if text, isText := v.(string); isText {
					v = sum(text)
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
