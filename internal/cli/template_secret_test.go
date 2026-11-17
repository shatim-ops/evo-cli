// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// A cloud-init template may carry a password or a token: the preview of
// evo image create names it by its size, lines and a known header,
// never by its text — in the human preview and in --json alike (the MCP
// answer: TestImageCreatePreview_TemplateSecretNotShown in internal/tools).
func TestImageCreatePreview_TemplateSecretNotShownInCLI(t *testing.T) {
	const secret = "TEMPLATESECRET42"
	_, fake := withJSONWorld(t)
	reg := newRegistry()
	tl, _ := reg.Lookup("image_create")
	cmd := commandsByTool()["image_create"]
	for _, tpl := range []string{
		"#cloud-config password: " + secret + "\nusers: []\n",
		"#cloud-config\nchpasswd:\n  list: root:" + secret + "\n",
	} {
		args := copyArgs(parityArgs["image_create"])
		args["user_data_template"] = tpl
		line := parityCommandOf(t, cmd.group, cmd.cmd, tl, args).tree
		for _, extra := range [][]string{{"--dry-run"}, {"--dry-run", "--json"}} {
			fake.reset()
			newProcess()
			_, out, errOut := runCLI(t, "", append(append([]string{}, extra...), line...)...)
			if !strings.Contains(out+errOut, "bytes, ") && !strings.Contains(out+errOut, "байт, ") {
				t.Fatalf("%v: no preview of the template: %s%s", extra, out, errOut)
			}
			if strings.Contains(out+errOut, secret) {
				t.Errorf("%v: the preview shows the template's text %q:\n%s%s", extra, tpl, out, errOut)
			}
		}
	}
}
