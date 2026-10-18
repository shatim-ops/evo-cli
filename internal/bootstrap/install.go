// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package bootstrap prepares a new VM over SSH: Docker with the compose
// plugin for the deploy user. Cloud.ru renders cloud-init only from the
// image's own user_data_template, so nothing here goes through cloud-init.
package bootstrap

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"
)

//go:embed install.sh
var installScript string

// InstallScript returns the shell script that installs Docker with the
// compose plugin, adds user to the docker group and gives it /opt/app.
// CRLF from a Windows checkout would break the script on the VM.
func InstallScript() string { return strings.ReplaceAll(installScript, "\r\n", "\n") }

// InstallCommand returns a remote command that runs InstallScript as root for
// user. The script goes through a temporary file, not `sh -s`: commands
// inside it (apt, curl | sh) would otherwise eat the rest of the script from
// stdin. sudo -n fails at once instead of waiting for a password.
func InstallCommand(user string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(InstallScript()))
	return fmt.Sprintf(`t=$(mktemp) && echo '%s' | base64 -d > "$t" && sudo -n sh "$t" %s; rc=$?; rm -f "$t"; exit $rc`,
		enc, shellQuote(user))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
