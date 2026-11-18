// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// The refusals evo words itself of a value of the wrong form, or of one
// outside a closed list, do not repeat it: a project ID that is no UUID,
// a tenant ID, a profile name, a group of tools, a client of evo agent
// setup, a port, EVO_LANG (release audit rc.1, A19-11, #2431; the owner's
// rule of #2459: the form, without the value).
func TestFormRefusals_DoNotRepeatTheValue(t *testing.T) {
	const secret = "s3cr3t-form-31415926535897"
	for _, c := range []struct {
		env  map[string]string
		args []string
	}{
		{nil, []string{"configure", "--project-id", secret}},
		{nil, []string{"configure", "s3", "--tenant-id", secret + ":x"}},
		{nil, []string{"--profile", secret + "!", "whoami"}},
		{map[string]string{"CLOUDRU_PROFILE": secret + " !"}, []string{"whoami"}},
		{nil, []string{"tools", "list", "--group", secret}},
		{nil, []string{"agent", "setup", "--client", secret}},
		{nil, []string{"tools", "call", "open_ports", "--arg", "security_group=web", "--arg", "ports=" + secret}},
		{map[string]string{"EVO_LANG": secret}, []string{"version"}},
	} {
		for _, extra := range [][]string{nil, {"--json"}} {
			setupCLI(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			newProcess()
			args := append(append([]string{}, extra...), c.args...)
			_, out, errOut := runCLI(t, "", args...)
			if strings.Contains(strings.ToLower(out+errOut), "s3cr3t") {
				t.Errorf("%v %v: the refusal repeats the value: %s%s", c.env, args, out, errOut)
			}
		}
	}
}
