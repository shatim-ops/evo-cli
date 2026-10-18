// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

func TestInstallScript_InstallsDockerAndPreparesUser(t *testing.T) {
	s := InstallScript()
	for _, want := range []string{
		"get.docker.com",
		"cloud-init status --wait",
		`usermod -aG docker "$user"`,
		`install -d -o "$user" /opt/app`,
		"docker compose version",
		// a failed first-boot apt mirror setup is re-run before get.docker.com
		"cloud-init single --name bootcmd --frequency always",
		"/var/lib/cloud/instance/sem/configure-apt-mirrors",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("install script lacks %q", want)
		}
	}
}

// The command carries the script intact and quotes the user name, so a
// hostile name from image metadata cannot inject shell.
func TestInstallCommand_EmbedsScriptAndQuotesUser(t *testing.T) {
	cmd := InstallCommand("x'; rm -rf / #")

	m := regexp.MustCompile(`echo '([A-Za-z0-9+/=]+)' \| base64 -d`).FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("no base64 payload in %q", cmd)
	}
	got, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil || string(got) != InstallScript() {
		t.Fatalf("payload does not decode to the script (err %v)", err)
	}
	if !strings.Contains(cmd, `sudo -n sh "$t" 'x'"'"'; rm -rf / #'`) {
		t.Errorf("user not quoted: %q", cmd)
	}
}
