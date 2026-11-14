// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ca_start and ca_stop answer the operation the platform gave them
// (AsyncOperation): ok when it is done, as shipped; starting or stopping
// with the state of the app and ca_get as the next step while it goes on;
// the platform's refusal when it ended with an error — never ok for what
// the platform has not done (Астра #1813; release plan 1.9, «Честный ход
// операций», (1)).
func TestCaStartStop_TheAnswerIsTheOperations(t *testing.T) {
	for _, tc := range []struct {
		tool, status string
	}{{"ca_start", "starting"}, {"ca_stop", "stopping"}} {
		op := `{"id":"op-1","done":true}`
		f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `{"name":"app-1","status":"deploying"}`)
				return true
			}
			_, _ = io.WriteString(w, op)
			return true
		}}
		r := apiFakeRegistry(t, f)
		args := map[string]any{"name": "app-1", "confirmed": true}

		m, text, isErr := callNamed(t, r, tc.tool, args)
		if isErr || m["status"] != "ok" || m["next_tool"] != nil {
			t.Errorf("%s, operation done: %s", tc.tool, text)
		}

		op = `{"id":"op-1","done":false}`
		m, text, isErr = callNamed(t, r, tc.tool, args)
		next, _ := m["next_args"].(map[string]any)
		if isErr || m["status"] != tc.status || m["app_status"] != "deploying" || m["next_tool"] != "ca_get" || next["name"] != "app-1" {
			t.Errorf("%s, operation going on: %s", tc.tool, text)
		}
		if !strings.Contains(fmt.Sprint(m["_untrusted"]), "app_status") {
			t.Errorf("%s: app_status of the answer is not listed in _untrusted: %s", tc.tool, text)
		}

		op = `{"id":"op-1","done":true,"error":{"code":9,"message":"quota"}}`
		m, text, isErr = callNamed(t, r, tc.tool, args)
		if !isErr || m["status"] == "ok" || !strings.Contains(text, "op-1") || !strings.Contains(text, "quota") {
			t.Errorf("%s, operation ended with an error: %s", tc.tool, text)
		}
	}
}
