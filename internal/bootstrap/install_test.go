// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"crypto/sha1" // an OpenPGP v4 fingerprint is SHA-1 by definition
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"
)

func TestInstallScript_InstallsDockerAndPreparesUser(t *testing.T) {
	// say "x" prints "evo: x".
	s := strings.ReplaceAll(InstallScript(), `say "`, `"evo: `)
	for _, want := range []string{
		// Ubuntu and Debian: Docker's apt repository with the embedded key
		"https://download.docker.com/linux/$os_id $codename stable",
		"signed-by=$docker_key",
		"-----BEGIN PGP PUBLIC KEY BLOCK-----",
		"docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		// short apt timeouts and a time limit on every apt-get
		"Acquire::http::Timeout=20",
		"Acquire::https::Timeout=20",
		"Acquire::Retries=2",
		`timeout "$limit" apt-get`,
		// the official mirrors, over HTTPS, signed by the distribution's keyring
		"https://archive.ubuntu.com/ubuntu",
		"https://security.ubuntu.com/ubuntu",
		"https://ports.ubuntu.com/ubuntu-ports",
		"https://deb.debian.org/debian",
		"https://deb.debian.org/debian-security",
		"[signed-by=$keyring]",
		// other distributions: get.docker.com, as before
		"curl -fsSL https://get.docker.com | sh",
		"cloud-init status --wait",
		`usermod -aG docker "$user"`,
		`install -d -o "$user" "$root/opt/app"`,
		"docker compose version",
		// a failed first-boot apt mirror setup is re-run before the fallback
		"cloud-init single --name bootcmd --frequency always",
		"/var/lib/cloud/instance/sem/configure-apt-mirrors",
		// what the caller's diagnosis looks for
		FallbackFailedLine,
		DockerRepoFailedLine,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("install script lacks %q", want)
		}
	}
	if strings.Contains(s, "@DOCKER_KEY@") {
		t.Error("the Docker key placeholder is left in the script")
	}
}

// Nothing in the script weakens apt's checks or takes packages over plain
// HTTP: every source is HTTPS and signed.
func TestInstallScript_NoUnsignedOrPlainHTTPSources(t *testing.T) {
	s := InstallScript()
	for _, bad := range []string{
		"trusted=yes", "allow-unauthenticated", "AllowInsecureRepositories", "AllowDowngradeToInsecureRepositories",
		"force-yes", "allow-insecure", "--insecure", "curl -k", "http://",
	} {
		if strings.Contains(strings.ToLower(s), strings.ToLower(bad)) {
			t.Errorf("install script contains %q", bad)
		}
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, `echo "deb `) && !strings.Contains(line, "https://") && !strings.Contains(line, "$base") && !strings.Contains(line, "$security") {
			t.Errorf("apt source without https: %s", line)
		}
	}
}

// The embedded key is Docker's release key: the fingerprint Docker
// publishes (docs.docker.com/engine/install/ubuntu), 9DC8 5822 9FC7 DD38
// 854A E2D8 8D81 803C 0EBF CD88. A changed key file fails here first.
func TestDockerKey_IsDockersReleaseKey(t *testing.T) {
	const want = "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"
	if got := primaryKeyFingerprint(t, dockerKey); got != want {
		t.Fatalf("docker.asc fingerprint = %s, want %s", got, want)
	}
	if !strings.Contains(InstallScript(), strings.ReplaceAll(dockerKey, "\r\n", "\n")) {
		t.Error("the script does not carry the key")
	}
}

// primaryKeyFingerprint decodes an ASCII-armoured OpenPGP key and returns
// the v4 fingerprint of its first packet (RFC 4880, 12.2).
func primaryKeyFingerprint(t *testing.T, armored string) string {
	t.Helper()
	var b64 strings.Builder
	inBody := false
	for _, line := range strings.Split(strings.ReplaceAll(armored, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "-----BEGIN PGP PUBLIC KEY BLOCK-----"):
			inBody = false
		case strings.HasPrefix(line, "-----END"), strings.HasPrefix(line, "="):
			inBody = false
		case !inBody && line == "":
			inBody = true // the blank line after the armour headers
		case inBody:
			b64.WriteString(line)
		}
	}
	data, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil || len(data) < 3 {
		t.Fatalf("armour: %v", err)
	}
	var body []byte
	switch tag := data[0]; {
	case tag&0xC0 == 0xC0: // new format
		if tag&0x3F != 6 {
			t.Fatalf("first packet tag %d, want 6 (public key)", tag&0x3F)
		}
		n, off := int(data[1]), 2
		switch {
		case n >= 192 && n < 224:
			n, off = (n-192)<<8+int(data[2])+192, 3
		case n == 255:
			n, off = int(data[2])<<24|int(data[3])<<16|int(data[4])<<8|int(data[5]), 6
		}
		body = data[off : off+n]
	case tag&0x80 == 0x80: // old format
		if (tag>>2)&0x0F != 6 {
			t.Fatalf("first packet tag %d, want 6 (public key)", (tag>>2)&0x0F)
		}
		var n, off int
		switch tag & 3 {
		case 0:
			n, off = int(data[1]), 2
		case 1:
			n, off = int(data[1])<<8|int(data[2]), 3
		case 2:
			n, off = int(data[1])<<24|int(data[2])<<16|int(data[3])<<8|int(data[4]), 5
		default:
			t.Fatal("indeterminate packet length")
		}
		body = data[off : off+n]
	default:
		t.Fatalf("not an OpenPGP packet: %#x", tag)
	}
	if body[0] != 4 {
		t.Fatalf("key version %d, want 4", body[0])
	}
	h := sha1.New()
	h.Write([]byte{0x99, byte(len(body) >> 8), byte(len(body))})
	h.Write(body)
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
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
