// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"fmt"
	"strings"
	"testing"
)

// cloud-e2e runs each night from main with the read-only key alone (plan
// 1.7, #717 p. 4): the job readonly — the reads of the API, the read-only
// tools, MCP over stdio and HTTP — and nothing that creates. The nightly
// run creates no resource and spends nothing, so it needs no approval; a
// job with the key of changes (environment cloud-e2e) or with a resource
// of its own is off on schedule. Its concurrency group is its own: a run
// waiting for the owner's approval does not hold the nightly reads back,
// and the nightly reads do not hold a run back. A red nightly run reaches
// the issue «main красный: cloud-e2e» (main-red.yml), as a red ci does.
func TestWorkflows_NightlyCloudIsReadOnly(t *testing.T) {
	ws := readWorkflows(t)
	w := ws["cloud-e2e.yml"]
	if s, _ := w.On["schedule"].([]any); len(s) != 1 {
		t.Fatalf("cloud-e2e: schedule %v, want one nightly run", w.On["schedule"])
	}
	if strings.ReplaceAll(w.Concurrency.Group, " ", "") != "${{github.event_name=='schedule'&&'cloud-readonly-nightly'||inputs.cleanup_only&&'cloud-e2e-cleanup'||'cloud-e2e'}}" {
		t.Errorf("cloud-e2e: concurrency group %q, want its own group for the nightly run", w.Concurrency.Group)
	}
	ro, ok := w.Jobs["readonly"]
	if !ok || ro.Environment != "cloud-readonly" || strings.Contains(ro.If, "schedule") {
		t.Errorf("cloud-e2e: job readonly (environment %q, if %q) does not run on schedule with the read-only key", ro.Environment, ro.If)
	}
	// off: the job does not run on schedule — its if says so, or it asks
	// for an input set to true (inputs are empty on schedule), or it needs
	// a job that is off.
	off := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for name, j := range w.Jobs {
			if off[name] {
				continue
			}
			o := strings.Contains(j.If, "github.event_name != 'schedule'") || strings.HasPrefix(j.If, "${{ inputs.")
			// A job skipped in its needs is skipped too, unless its if
			// runs it anyway (always(), !cancelled()).
			if !strings.Contains(j.If, "always()") && !strings.Contains(j.If, "cancelled()") {
				for _, n := range needsOf(j.Needs) {
					o = o || off[n]
				}
			}
			if o {
				off[name], changed = true, true
			}
		}
	}
	for name := range w.Jobs {
		if name != "readonly" && !off[name] {
			t.Errorf("cloud-e2e: job %s runs on schedule; the nightly run is the job readonly alone", name)
		}
	}

	red := ws["main-red.yml"]
	trig, _ := red.On["workflow_run"].(map[string]any)
	if fmt.Sprint(trig["workflows"]) != "[ci cloud-e2e]" {
		t.Errorf("main-red: follows %v, want ci and cloud-e2e", trig["workflows"])
	}
	for name, j := range red.Jobs {
		for _, s := range j.Steps {
			if strings.Contains(s.Run, "main_red.sh") && s.Env["WORKFLOW"] != "${{ github.event.workflow_run.name }}" {
				t.Errorf("main-red: job %s does not name the workflow of the run (WORKFLOW %q): a red cloud-e2e would be filed as ci", name, s.Env["WORKFLOW"])
			}
		}
	}
}

func needsOf(n any) []string {
	switch v := n.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			out = append(out, fmt.Sprint(x))
		}
		return out
	}
	return nil
}
