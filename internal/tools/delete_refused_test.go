// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

// refusedState is a state in which the platform refuses the delete of a
// resource that is there: the state the resource reads in, the answer to
// its delete, and the item of the register of discrepancies that saw it
// live. A passing state (creating, DEPLOYING) ends by itself, and the way
// out is to wait and delete again; a final one (FAILED, error_deleting)
// does not, and the way out is support or the console.
type refusedState struct {
	state, message, register string
	code                     int
	passing                  bool
}

// deleteRefusals declares, for every delete of deleteTargets, the states in
// which the platform refuses it (CLAUDE.md §6, axis 3: such an answer says
// so and names the way out — support, the console — and is no success). A
// delete with none known says why: the list is
// closed, so a new delete without a line fails the build.
var deleteRefusals = map[string]struct {
	states []refusedState
	none   string // why no state is declared
}{
	"destroy": {states: []refusedState{{state: "creating", code: http.StatusConflict,
		message: "VM in status creating cannot be deleted", register: "CMP-7", passing: true}}},
	"release_floating_ip": {states: []refusedState{{state: "error_deleting", code: http.StatusUnprocessableEntity,
		message: "cannot delete floating ip in status error_deleting", register: "NET-7"}}},
	"snat_delete": {states: []refusedState{{state: "creating", code: http.StatusUnprocessableEntity,
		message: "Failed to delete NAT gateway with current status", register: "NET-11", passing: true}}},
	"kafka_delete": {states: []refusedState{
		{state: "DEPLOYING", code: http.StatusInternalServerError,
			message: "call to paas engine [DeleteEvolutionKafka] failed", register: "DB-17", passing: true},
		{state: "FAILED", code: http.StatusInternalServerError,
			message: "call to paas engine [DeleteEvolutionKafka] failed", register: "DB-17"}}},
	"redis_delete": {states: []refusedState{{state: "DEPLOYING", code: http.StatusBadRequest,
		message: "cluster is deploying", register: "DB-19", passing: true}}},
	"pg_delete": {states: []refusedState{{state: "FAILED", code: http.StatusInternalServerError,
		message: `service with "FAILED" status may be delete only with force flag`, register: "DB-21"}}},

	"ar_delete":         {none: "no refusal seen live (register, 28.10)"},
	"close_ports":       {none: "a rule has no state"},
	"ca_delete":         {none: "no refusal seen live; a repeat during the deletion is a new operation (CAP-4)"},
	"cert_delete":       {none: "no refusal seen live (register, 28.10)"},
	"disk_delete":       {none: "an attached disk is refused before the question (disk_delete reads it)"},
	"dns_record_delete": {none: "a record has no state"},
	"dns_zone_delete":   {none: "a zone in creating is deleted, slowly (NET-13): no refusal"},
	"image_delete":      {none: "no refusal seen live (register, 28.10)"},
	"k8s_delete":        {none: "no refusal seen live (register, 28.10)"},
	"lb_delete":         {none: "no refusal seen live (register, 28.10)"},
	"pg_db_delete":      {none: "lives in a cluster: the cluster's state is pg_delete's"},
	"pg_user_delete":    {none: "lives in a cluster: the cluster's state is pg_delete's"},
	"s3_bucket_delete":  {none: "a bucket with objects is refused before the question (D26 world: s3Parity)"},
	"s3_rm":             {none: "an object has no state"},
	"secret_delete":     {none: "no refusal seen live (register, 28.10)"},
	"tg_delete":         {none: "no refusal seen live (register, 28.10)"},
	"vpc_delete":        {none: "no refusal seen live (register, 28.10)"},
	"vpc_route_delete":  {none: "a route has no state"},
}

// refusingWorld has the resource liveID in state s, in its own answer and
// in every list, and refuses every write the way the platform does in s.
func refusingWorld(s refusedState, writes *atomic.Int32) func(http.ResponseWriter, *http.Request) bool {
	obj := fmt.Sprintf(`{"id":%q,"name":%q,"status":%q,"state":%q,"project_id":%q,"projectId":%q}`,
		liveID, liveName, s.state, strings.ToLower(s.state), dbaasProject, dbaasProject)
	return func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writes.Add(1)
			w.WriteHeader(s.code)
			_, _ = fmt.Fprintf(w, `{"code":9,"message":%q}`, s.message)
			return true
		}
		body := strings.TrimSuffix(obj, "}") + `,"items":[` + obj + `],"total":1,"clusters":[` + obj + `]}`
		_, _ = io.WriteString(w, body)
		return true
	}
}

// waysOut are the words of a hint that names the way out of a refused
// delete, in either language of the CLI: of a passing state, to wait; of
// a final one, support or the console.
var waysOut = map[bool][]string{
	true:  {"wait", "подожд", "дожд"},
	false: {"support", "console", "поддержк", "консол"},
}

// CLAUDE.md §6, axis 3: «Удалить нельзя — ответ говорит это прямо и
// называет путь (поддержка, консоль), а не выглядит успехом». Every
// declared refusal: the resource is there in the state, the delete with
// consent meets the platform's refusal, and the answer is an error with
// a hint that names the way out — to wait, of a passing state; support or
// the console, of a final one. Its reason stays API_ERROR: a new one would
// be a breaking change (owner's decision 28.10, #455, option (б)). Release plan 1.5, «мир „платформа отвергает удаление в состоянии
// X“»; live: DB-17 (Kafka in DEPLOYING and FAILED), DB-21 (PostgreSQL in
// FAILED).
func TestDelete_RefusedInStateSaysSoAndTheWayOut(t *testing.T) {
	names := make([]string, 0, len(deleteTargets))
	for name := range deleteTargets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		decl, ok := deleteRefusals[name]
		switch {
		case !ok:
			t.Errorf("%s: no line in deleteRefusals — declare the states the platform refuses its delete in, or why there are none", name)
			continue
		case len(decl.states) == 0 && decl.none == "":
			t.Errorf("%s: deleteRefusals declares no state and no reason", name)
			continue
		}
		d := deleteTargets[name]
		for _, s := range decl.states {
			var writes atomic.Int32
			r := erasedRegistry(t, refusingWorld(s, &writes))
			args := goneArgs(name, d)
			args[d.arg] = liveID
			args["confirmed"] = true
			m, text, isErr := callNamed(t, r, name, args)
			where := fmt.Sprintf("%s in %s (%s)", name, s.state, s.register)
			if !isErr {
				t.Errorf("%s: the platform refused the delete, the answer is a success: %s", where, clipText(text))
				continue
			}
			hint, _ := m["hint"].(string)
			named := false
			for _, w := range waysOut[s.passing] {
				named = named || strings.Contains(strings.ToLower(hint), w)
			}
			if !named {
				t.Errorf("%s: the hint names no way out (%v): %q", where, waysOut[s.passing], hint)
			}
		}
	}
	for name := range deleteRefusals {
		if _, ok := deleteTargets[name]; !ok {
			t.Errorf("deleteRefusals names %s, which is no delete of deleteTargets", name)
		}
	}
}

func clipText(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
