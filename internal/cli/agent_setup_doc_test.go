// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"strings"
	"testing"
)

// docs/agent-setup.md is the prompt an agent follows to get access. The
// rule on the link evo login prints is one an agent can keep: it does not
// retell, send or open it, and the person opens it from the terminal on the
// same machine (decision of the owner 17.11, #2057). «Не вставляй в чат» alone
// left the agent free to open the link with its own browser tool.
func TestAgentSetup_LoginLinkRuleIsExecutable(t *testing.T) {
	b, err := os.ReadFile("../../docs/agent-setup.md")
	if err != nil {
		t.Fatal(err)
	}
	// The text is wrapped: a phrase may break at any space.
	doc := strings.Join(strings.Fields(string(b)), " ")
	for _, want := range []string{
		"не пересказывай, не отправляй и не открывай",
		"человек открывает её сам из терминала на этой же машине",
		// The failure step shows evo's error, not its output with the link
		// («Риск», conclusion 282, У1).
		"ошибку и подсказку, которые напечатал evo, — без ссылки входа",
		// A person who cannot see the agent's terminal runs evo login
		// themselves (282, У2).
		"попроси человека самому выполнить `evo login --for agent`",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/agent-setup.md does not say %q", want)
		}
	}
}
