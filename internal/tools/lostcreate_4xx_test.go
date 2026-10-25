// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/dns"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// lostCreate reconciled every failed create by name: a secret_create on a
// path that exists got the API's refusal, found the secret of the path in
// the list and answered "creating … exists and is billed", isError false
// (Grok live run on 1.3.0, G130-15; DECISIONS D42). A refusal the API gave
// on purpose — a 4xx — is not a lost answer: the request was read and
// declined, nothing was made. Only an answer that did not come back (no
// status at all, a 5xx a gateway may have given for a backend that applied
// the request) or a success without an id is reconciled by name.
func TestLostCreate_RefusalIsAnErrorNotCreating(t *testing.T) {
	taken := func(context.Context) ([]map[string]any, error) {
		return []map[string]any{{"id": "00000000-0000-4000-8000-0000000000a1", "name": "app/db", "status": "RUNNING"}}, nil
	}
	refused := []struct {
		name   string
		err    error
		reason string
	}{
		{"409 compute", fmt.Errorf("create disk: %w", &provision.StatusError{Status: 409, Body: "already exists"}), clierrors.CodeAPIError},
		{"400 service client", fmt.Errorf("create secret: %w", errors.New("secret API 400: path already exists")), clierrors.CodeAPIError},
		{"403 dns", fmt.Errorf("create zone: %w", &dns.APIError{Status: 403, Message: "forbidden"}), clierrors.CodeAPIError},
		{"404 parent gone", fmt.Errorf("create record: %w", errors.New("dns API 404: zone not found")), clierrors.CodeNotFound},
		{"422 compute", fmt.Errorf("create image: %w", &provision.StatusError{Status: 422, Body: "disk is not available"}), clierrors.CodeAPIError},
		{"429", fmt.Errorf("create cluster: %w", errors.New("dbaas API 429: ResourceExhausted")), clierrors.CodeAPIRateLimited},
	}
	for _, c := range refused {
		t.Run("refused/"+c.name, func(t *testing.T) {
			text, isErr := lostCreate(context.Background(), "secret_create", taken, "secret", "app/db", nil, "secret_get path", c.err)
			m := parseAnswer(t, text)
			if !isErr {
				t.Errorf("isError false: %s", text)
			}
			if m["reason"] != c.reason {
				t.Errorf("reason %v, want %s: %s", m["reason"], c.reason, text)
			}
			for _, word := range []string{`"status":"creating"`, "is billed", "secret_id"} {
				if strings.Contains(text, word) {
					t.Errorf("a refused create answered as made (%s): %s", word, text)
				}
			}
		})
	}
	lost := []struct {
		name string
		err  error
	}{
		{"502 gateway", fmt.Errorf("create secret: %w", errors.New("secret API 502: bad gateway"))},
		{"504 compute", fmt.Errorf("create disk: %w", &provision.StatusError{Status: 504, Body: "timeout"})},
		{"cut body", fmt.Errorf("create secret: %w", io.ErrUnexpectedEOF)},
		{"no connection", fmt.Errorf("create secret: %w", &net.OpError{Op: "dial", Err: errors.New("connection reset")})},
		{"deadline", fmt.Errorf("create secret: %w", context.DeadlineExceeded)},
		{"success without id", fmt.Errorf("create secret: %w", noID("secret"))},
	}
	for _, c := range lost {
		t.Run("lost/"+c.name, func(t *testing.T) {
			text, isErr := lostCreate(context.Background(), "secret_create", taken, "secret", "app/db", nil, "secret_get path", c.err)
			m := parseAnswer(t, text)
			if isErr || m["status"] != "creating" || m["secret_id"] != "00000000-0000-4000-8000-0000000000a1" {
				t.Errorf("a lost answer with the secret in the list must name it as made: isErr=%v %s", isErr, text)
			}
		})
	}
}

// The same through the handler and the HTTP fault world, which refuses a
// second secret of a path with 409 as the Secret Manager does: the answer
// is an error with the API's reason, not a "creating" with the id of the
// secret that was there before (G130-15).
func TestSecretCreate_TakenPathIsAnErrorNotCreating(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &faultAPI{cancel: cancel, made: []faultResource{
		{host: "secretmanager.api.cloud.ru", path: "/v2/secret", ID: "00000000-0000-4000-8000-0000000000a1", Name: "app/db", before: true},
	}}
	r := faultAPIRegistry(t, a)
	text, isErr := r.handlers["secret_create"](ctx, map[string]any{"path": "app/db", "value": "x", "confirmed": true})
	m := parseAnswer(t, text)
	if !isErr || m["reason"] != clierrors.CodeAPIError {
		t.Errorf("secret_create on a taken path: isErr=%v reason=%v, want an API_ERROR: %s", isErr, m["reason"], text)
	}
	if strings.Contains(text, "is billed") || m["status"] == "creating" {
		t.Errorf("the API's refusal read as a lost answer: %s", text)
	}
	if n := len(a.billed()); n != 0 {
		t.Errorf("the refused create made %d secrets, want none", n)
	}
}

func parseAnswer(t *testing.T, text string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("answer is not JSON: %v: %s", err, text)
	}
	return m
}
