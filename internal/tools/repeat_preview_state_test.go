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

// A repeat of k8s_add_addon on a cluster that has the addon asks as before
// — its exit code does not change before the major — and the question
// says what this call does to the installed one, by the resolve_mode it
// sends (release plan 1.9, «Повтор у выпущенных инструментов», п. 4).
func TestK8sAddAddon_QuestionSaysWhatHappensToTheInstalled(t *testing.T) {
	for _, c := range []struct {
		mode, installed string
		says, not       string
	}{
		{"overwrite", "cert-manager", "resolve_mode OVERWRITE replaces its objects", "may fail"},
		{"", "cert-manager", "with resolve_mode NONE its objects are kept, and the install may fail", "replaces"},
		{"overwrite", "traefik", "", "already installed"},
	} {
		f := &fakeMK8s{kubeVersion: "v1.30.2", pools: `{"nodePools":[{"scalePolicy":{"autoScale":{"minCount":"1","maxCount":"3"}}}]}`,
			installed: `{"clusterAddons":[{"id":"ca-0","name":"` + c.installed + `"}]}`}
		args := map[string]any{"cluster_id": addonCluster, "name": "cert-manager", "version": "1.13.0"}
		if c.mode != "" {
			args["resolve_mode"] = c.mode
		}
		m, text, _ := callNamed(t, addonRegistry(t, f), "k8s_add_addon", args)
		warnings := fmt.Sprint(m["warnings"])
		if m["status"] != "confirmation_required" || (c.says != "" && !strings.Contains(warnings, c.says)) || strings.Contains(warnings, c.not) {
			t.Errorf("mode %q, installed %s: warnings %s\n%s", c.mode, c.installed, warnings, text)
		}
		if len(f.posts) != 0 {
			t.Errorf("the question wrote: %v", f.posts)
		}
	}
}

// The question of ca_start and ca_stop names the state of the app as the
// API gives it, read for the question only: a call with consent reads
// nothing more. A state that cannot be read is said in the question, not
// refused — the call's own refusal stays the platform's, after consent,
// as it was (release plan 1.9, «Повтор у выпущенных инструментов», п. 4).
func TestCaStartStop_QuestionNamesTheStateOfTheApp(t *testing.T) {
	for _, tool := range []string{"ca_start", "ca_stop"} {
		status := http.StatusOK
		f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method != http.MethodGet {
				_, _ = io.WriteString(w, `{}`)
				return true
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"name":"app-1","status":"running"}`)
			return true
		}}
		r := apiFakeRegistry(t, f)
		m, text, _ := callNamed(t, r, tool, map[string]any{"name": "app-1"})
		if m["status"] != "confirmation_required" || m["app_status"] != "running" || m["app_status_note"] != nil {
			t.Errorf("%s: the question does not name the state: %s", tool, text)
		}
		for _, q := range f.calls {
			if !strings.HasPrefix(q, "GET ") {
				t.Errorf("%s: the question sent %s", tool, q)
			}
		}
		status = http.StatusForbidden
		m, text, _ = callNamed(t, r, tool, map[string]any{"name": "app-1"})
		if note, _ := m["app_status_note"].(string); m["status"] != "confirmation_required" || m["app_status"] != nil || !strings.Contains(note, "could not be read") {
			t.Errorf("%s: a state that cannot be read: %s", tool, text)
		}
		f.calls = nil
		status = http.StatusOK
		if _, text, isErr := callNamed(t, r, tool, map[string]any{"name": "app-1", "confirmed": true}); isErr {
			t.Errorf("%s with consent: %s", tool, text)
		}
		for _, q := range f.calls {
			if strings.HasPrefix(q, "GET ") {
				t.Errorf("%s with consent read %s: the state is read for the question only", tool, q)
			}
		}
	}
}
