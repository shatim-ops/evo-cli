// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Grok live run on v1.3.0, 26/27.10: a PostgreSQL or Kafka cluster deleted
// earlier stays readable in ERASED; a second pg_delete or kafka_delete then
// asked for consent and met 500 FailedPrecondition, with the false hint
// that the cluster was still being created (N1). A delete of a FAILED
// Kafka cluster warned in the preview and then answered the platform's bare
// 500 «failed to get delete metadata» (N2). The state read before the
// question decides both, in deletePlan, the one plan of the DBaaS and
// network deletes: ERASED or DELETED is already deleted (D26), without the
// question and without a DELETE; FAILED is said after the refusal as it is
// before the question — the platform does not delete it, the console or
// support do (CLAUDE.md §6, axis 3).

func planAnswer(t *testing.T, p *Plan) map[string]any {
	t.Helper()
	if p.Execute != nil {
		t.Fatalf("the plan asks for consent; want an answer without the question: %v", p.Preview)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(p.answer), &m); err != nil {
		t.Fatalf("answer is not JSON: %s", p.answer)
	}
	return m
}

func TestDeletePlan_ErasedIsAlreadyDeleted(t *testing.T) {
	for _, status := range []string{"ERASED", "erased", "DELETED"} {
		t.Run(status, func(t *testing.T) {
			r := newTestServer()
			deletes := 0
			p := r.deletePlan("pg_delete", "pg_delete deletes %s.", "pg_delete удаляет %s.",
				"00000000-0000-4000-8000-0000000000e1",
				func() (map[string]any, error) {
					return map[string]any{"id": "00000000-0000-4000-8000-0000000000e1", "name": "pg-1", "status": status}, nil
				},
				func(context.Context) (string, bool) {
					deletes++
					return errResult(errors.New("dbaas API 500: FailedPrecondition")), true
				})
			m := planAnswer(t, p)
			if m["status"] != "already_deleted" || p.answerErr {
				t.Errorf("a cluster in %s: want already_deleted, got %v", status, m)
			}
			if deletes != 0 {
				t.Errorf("a DELETE was sent for a cluster in %s", status)
			}
		})
	}
}

func TestDeletePlan_FailedRefusalSaysWhereToDelete(t *testing.T) {
	r := newTestServer()
	id := "00000000-0000-4000-8000-0000000000f9"
	p := r.deletePlan("kafka_delete", "kafka_delete deletes %s.", "kafka_delete удаляет %s.", id,
		func() (map[string]any, error) {
			return map[string]any{"id": id, "name": "kf-1", "status": "FAILED"}, nil
		},
		func(context.Context) (string, bool) {
			return errResult(errors.New("delete cluster: dbaas API 500: failed to get delete metadata")), true
		})
	if p.Execute == nil {
		t.Fatalf("a FAILED cluster still asks: the platform may delete it")
	}
	text, isErr := p.Execute(context.Background())
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatal(err)
	}
	hint, _ := m["hint"].(string)
	if !isErr || m["reason"] != clierrors.CodeAPIError || m["status"] == "deleted" {
		t.Errorf("a refused delete of a FAILED cluster must stay an error: %s", text)
	}
	if !strings.Contains(hint, "console") || !strings.Contains(hint, "support") || !strings.Contains(hint, id) ||
		strings.Contains(hint, "being created") {
		t.Errorf("the refusal must name the console and support with the id, not «being created»: %q", hint)
	}
	if m["resource_status"] != "FAILED" {
		t.Errorf("the answer must name the cluster's status: %s", text)
	}
	// A delete the platform accepts is answered as it is.
	p = r.deletePlan("kafka_delete", "kafka_delete deletes %s.", "kafka_delete удаляет %s.", id,
		func() (map[string]any, error) { return map[string]any{"id": id, "status": "FAILED"}, nil },
		func(context.Context) (string, bool) { return toJSON(map[string]any{"status": "deleting"}), false })
	if text, isErr := p.Execute(context.Background()); isErr || !strings.Contains(text, "deleting") {
		t.Errorf("an accepted delete of a FAILED cluster: %s", text)
	}
}

// Full audit 1.5, FB150-2: the way out of a status is the answer to the
// platform's refusal only. An expired key (401), a missing role (403), a
// rate limit (429) or a cut connection on the DELETE of a resource read in
// FAILED or CREATING keeps the hint of its own reason: «delete it in the
// console» does not fix an expired key. resource_status stays: the status
// read before the question is still true, and 1.5 answers with it, so
// dropping it would be a breaking change.
func TestDeletePlan_WayOutOnlyForTheRefusal(t *testing.T) {
	r := newTestServer()
	id := "00000000-0000-4000-8000-0000000000fa"
	for _, status := range []string{"FAILED", "ERROR", "ERROR_DELETING", "CREATING", "DEPLOYING", "PROVISIONING"} {
		for _, code := range []string{clierrors.CodeAuthRequired, clierrors.CodeAuthExpired, clierrors.CodeAuthFailed,
			clierrors.CodeAPIRateLimited, clierrors.CodeAPIUnreachable} {
			own := "the hint of " + code
			p := r.deletePlan("kafka_delete", "kafka_delete deletes %s.", "kafka_delete удаляет %s.", id,
				func() (map[string]any, error) { return map[string]any{"id": id, "status": status}, nil },
				func(context.Context) (string, bool) {
					return errResult(clierrors.New(code, "delete cluster: "+code, own)), true
				})
			if p.Execute == nil {
				t.Fatalf("%s: no execute", status)
			}
			text, _ := p.Execute(context.Background())
			var m map[string]any
			if err := json.Unmarshal([]byte(text), &m); err != nil {
				t.Fatal(err)
			}
			if m["hint"] != own || m["resource_status"] != status {
				t.Errorf("%s in %s: hint %q, resource_status %v; want the hint of the reason and the status read before the question", code, status, m["hint"], m["resource_status"])
			}
		}
	}
}

// notTheState names every reason of the key and of the connection: a
// new AUTH_* or API_* code of internal/errors is placed by a line here, so
// it does not get the hint of a status by default (FB150-2, review of #539).
func TestNotTheState_EveryKeyAndConnectionReason(t *testing.T) {
	theRefusal := map[string]string{
		clierrors.CodeAPIError: "the platform's refusal: the status explains it",
	}
	src, err := os.ReadFile(filepath.Join("..", "errors", "codes.go"))
	if err != nil {
		t.Fatal(err)
	}
	codes := regexp.MustCompile(`= "((?:AUTH|API)_[A-Z_]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(codes) == 0 {
		t.Fatal("no AUTH_* or API_* code in internal/errors/codes.go")
	}
	for _, c := range codes {
		if !notTheState[c[1]] && theRefusal[c[1]] == "" {
			t.Errorf("%s: neither in notTheState nor a refusal the status explains", c[1])
		}
	}
}
