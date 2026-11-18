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

// An object given to --arg that is not JSON is refused by the kind and
// place of the error, as --args-json (#2437), not by the decoder's text,
// which quotes a character of the value (release audit rc.1, A19-28).
func TestArgObject_JSONRefusalIsTheOnePoint(t *testing.T) {
	setupCLI(t)
	for _, extra := range [][]string{nil, {"--json"}} {
		newProcess()
		args := append(append([]string{}, extra...), "tools", "call", "k8s_add_addon",
			"--arg", "cluster_id=00000000-0000-4000-8000-000000000001", "--arg", `config={"a":Q}`)
		code, out, errOut := runCLI(t, "", args...)
		if code == 0 || strings.Contains(out+errOut, "invalid character") || strings.Contains(out+errOut, "'Q'") {
			t.Errorf("%v: want the kind and place of the error, not the decoder's text: %s%s", extra, out, errOut)
		}
	}
}
