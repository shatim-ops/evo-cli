// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// tariffPages are the tariff pages of the billable creates that 1.2.3 gave
// tariff_url, each checked by a live request before it went in (200, the
// page names the service; PR of 1.2.3). The test pins the page, so a link
// does not move to another service unnoticed.
var tariffPages = map[string]string{
	"ar_create":    "https://cloud.ru/documents/tariffs/evolution/artifact-registry",
	"ca_deploy":    "https://cloud.ru/documents/tariffs/evolution/container-apps",
	"kafka_create": "https://cloud.ru/documents/tariffs/evolution/managed-kafka",
	"lb_create":    "https://cloud.ru/documents/tariffs/evolution/load-balancer",
	"redis_create": "https://cloud.ru/documents/tariffs/evolution/managed-redis",
	"snat_create":  "https://cloud.ru/documents/tariffs/evolution/evolution-compute",
}

// The person sees what the agent sees: the CLI preview of a billable create
// names its tariff, and the warnings of the tool's answer (privileged root,
// a single broker, the hosts of one zone) are not lost when the preview is
// shown in the words of deploy.
func TestPreview_TariffAndWarningsInCLI(t *testing.T) {
	withParityWorld(t)
	srv := parityServers(t)
	russian(t)
	reg, commands := tools.New(nil), commandsByTool()
	for name, page := range tariffPages {
		args := copyArgs(parityArgs[name])
		if name == "ca_deploy" {
			args["privileged"] = true
		}
		got := stdioDetails(t, srv.stdio, name, args)
		p, _ := got["preview"].(map[string]any)
		if p["tariff_url"] != page {
			t.Errorf("%s: tariff_url %v, want %s", name, p["tariff_url"], page)
		}
		tl, _ := reg.Lookup(name)
		tc, ok := commands[name]
		if !ok {
			t.Errorf("%s: no command of the trees", name)
			continue
		}
		// The command of the tree shows the preview to a person; 'evo
		// tools call' prints the answer's fields as they are.
		pc := parityCommandOf(t, tc.group, tc.cmd, tl, args)
		newProcess()
		code, _, errOut := runCLI(t, "", pc.tree...)
		if code != clerrors.ExitConfirmationRequired {
			t.Errorf("evo %s: exit %d, want 3: %s", strings.Join(pc.tree, " "), code, clipLine(errOut))
			continue
		}
		if strings.Contains(errOut, `"tariff_url"`) {
			t.Errorf("evo %s: the preview as JSON, not in words:\n%s", strings.Join(pc.tree, " "), errOut)
		}
		if !strings.Contains(errOut, page) {
			t.Errorf("%s: the CLI preview does not name the tariff %s:\n%s", name, page, errOut)
		}
		for _, w := range answerWarnings(got) {
			if !strings.Contains(errOut, w) {
				t.Errorf("%s: the CLI preview lost the warning %q:\n%s", name, w, errOut)
			}
		}
		if name == "ca_deploy" && len(answerWarnings(got)) == 0 {
			t.Errorf("ca_deploy privileged: no warning in the answer: %v", got)
		}
	}
}

// answerWarnings are the warnings of a confirmation_required answer.
func answerWarnings(got map[string]any) []string {
	var out []string
	if w, ok := got["warning"].(string); ok && w != "" {
		out = append(out, w)
	}
	if ws, ok := got["warnings"].([]any); ok {
		for _, w := range ws {
			out = append(out, fmt.Sprint(w))
		}
	}
	if p, ok := got["preview"].(map[string]any); ok {
		if ws, ok := p["warnings"].([]any); ok {
			for _, w := range ws {
				out = append(out, fmt.Sprint(w))
			}
		}
	}
	return out
}
