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

// dockerKey is Docker's apt signing key, as download.docker.com/linux/ubuntu/gpg
// serves it (the same file for Debian). Embedded, so the VM does not take
// the key from the network it installs from; install_test.go checks its
// fingerprint.
//
//go:embed docker.asc
var dockerKey string

// Lines the install script prints when apt fails, for the caller's diagnosis.
const (
	// FallbackFailedLine: neither the image's apt mirror nor the
	// distribution's official mirror answered.
	FallbackFailedLine = "evo: the official mirror failed too"
	// DockerRepoFailedLine: the distribution's packages came, Docker's
	// repository did not (no answer, or a signature that does not verify).
	DockerRepoFailedLine = "evo: apt failed on Docker's repository (download.docker.com)"
)

// InstallScript returns the shell script that installs Docker with the
// compose plugin, adds user to the docker group and gives it /opt/app.
// CRLF from a Windows checkout would break the script on the VM.
func InstallScript() string {
	key := strings.ReplaceAll(dockerKey, "\r\n", "\n")
	return strings.Replace(strings.ReplaceAll(installScript, "\r\n", "\n"), "@DOCKER_KEY@\n", key, 1)
}

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
