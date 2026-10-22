// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// User SSH access used to be a manual post-install step ("append their
// public key to authorized_keys") that was routinely forgotten — VMs came up
// reachable only by the agent. deploy_apply now closes that loop itself:
// every public key listed in <state>/.evo/user_authorized_keys is
// appended (deduplicated) to ~/.ssh/authorized_keys on the new VM over the
// agent's own deploy key. The file is one authorized_keys-format line per key;
// '#' comments and blank lines are skipped. Missing/empty file = no-op, and
// any failure is reported in the deploy note rather than failing the deploy —
// the VM is already up, access repair must not destroy it.

const userKeysFile = "user_authorized_keys"

// legacyUserKeysFile is the file's name before v1.0; it is read when the new
// one is absent, until v2.0 (BACKLOG [10]).
const legacyUserKeysFile = "captain_authorized_keys"

// loadUserKeys returns the user public keys configured for auto-append and
// the name of the file they came from. Lines that are not authorized_keys
// entries are left out; deploy_plan shows them (loadUserKeyLines).
func loadUserKeys(stateDir string) ([]string, string) {
	keys, _, name := loadUserKeyLines(stateDir)
	return keys, name
}

// rejectedKeyLine is a line of the keys file that is not sent to the VM.
type rejectedKeyLine struct {
	first, last int    // line numbers, 1-based
	why         string // without the line's text: it may be a private key
}

// loadUserKeyLines reads the keys file: the authorized_keys entries, with
// their options (command="…", from="…", no-agent-forwarding), as
// ssh.ParseAuthorizedKey accepts them, and the lines it does not accept.
// A pasted private key or arbitrary text must never reach a VM's
// authorized_keys; until break r4 (R4-10) such lines, and option-prefixed
// keys with them, were dropped without a word.
func loadUserKeyLines(stateDir string) (keys []string, rejected []rejectedKeyLine, name string) {
	dir := config.DataDir(stateDir)
	name = userKeysFile
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		name = legacyUserKeysFile
		raw, err = os.ReadFile(filepath.Join(dir, name))
	}
	if err != nil {
		return nil, nil, userKeysFile
	}
	lines := strings.Split(string(raw), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-----BEGIN") {
			// A PEM block: one entry for the whole block, up to its END line.
			r := rejectedKeyLine{first: i + 1, last: i + 1, why: "not a public key"}
			if strings.Contains(line, "PRIVATE KEY") {
				r.why = "a PRIVATE key: remove it from the file and keep it secret"
			}
			for j := i + 1; j < len(lines); j++ {
				if strings.HasPrefix(strings.TrimSpace(lines[j]), "-----END") {
					r.last, i = j+1, j
					break
				}
			}
			rejected = append(rejected, r)
			continue
		}
		if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line)); err != nil {
			why := "not an authorized_keys entry"
			if t := strings.Fields(line)[0]; knownKeyType(t) {
				why = "not a valid " + t + " public key"
			}
			rejected = append(rejected, rejectedKeyLine{first: i + 1, last: i + 1, why: why})
			continue
		}
		keys = append(keys, line)
	}
	return keys, rejected, name
}

// knownKeyType reports whether word names an SSH public key algorithm, so
// that an error may quote it: any other first word may be a piece of a
// secret pasted by mistake.
func knownKeyType(word string) bool {
	for _, prefix := range []string{"ssh-ed25519", "ssh-rsa", "ssh-dss", "ecdsa-sha2-", "sk-ssh-ed25519@", "sk-ecdsa-sha2-"} {
		if strings.HasPrefix(word, prefix) {
			return true
		}
	}
	return false
}

// describeUserKeys is what deploy_plan shows of the keys deploy_apply will
// append: type, SHA256 fingerprint, comment and options (command="…" runs
// instead of the login shell) of each, then the lines it will not send, with
// error. The file may have come with a repository someone else wrote (a
// template with .evo/ in it): the user approving the plan sees whose keys
// get a login on the VM (security audit 19.10, follow-up of SEC-2).
func describeUserKeys(keys []string, rejected []rejectedKeyLine) []map[string]any {
	out := make([]map[string]any, 0, len(keys)+len(rejected))
	for _, k := range keys {
		pub, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(k))
		if err != nil { // loadUserKeyLines parsed it already
			out = append(out, map[string]any{"error": "not an authorized_keys entry; not sent to the VM"})
			continue
		}
		e := map[string]any{"type": pub.Type(), "fingerprint": ssh.FingerprintSHA256(pub), "comment": comment}
		if len(options) > 0 {
			e["options"] = options
		}
		out = append(out, e)
	}
	for _, r := range rejected {
		where := fmt.Sprintf("line %d", r.first)
		if r.last > r.first {
			where = fmt.Sprintf("lines %d-%d", r.first, r.last)
		}
		out = append(out, map[string]any{"error": where + ": " + r.why + "; not sent to the VM"})
	}
	return out
}

// appendUserKeys SSHes into the freshly created VM with the agent's deploy
// key and appends each user key to authorized_keys (idempotent). The VM's
// sshd may lag VM-active by a few seconds, so connection is retried briefly.
// It returns the VM's host key (known_hosts form) seen on that first,
// trust-on-first-use connection, for the project's known_hosts.
func appendUserKeys(ctx context.Context, host, login string, agentKeyPEM []byte, keys []string) (hostKey string, err error) {
	if len(keys) == 0 {
		return "", nil
	}

	script := userKeysScript(keys)

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(10 * time.Second):
			}
		}
		d := deploy.NewSSHDeployer()
		if err := d.Connect(ctx, host, 22, login, agentKeyPEM, ""); err != nil {
			lastErr = err
			continue
		}
		_, err := d.Exec(ctx, script)
		_ = d.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return d.HostKeyLine(), nil
	}
	return "", lastErr
}

// userKeysScript is the shell command that appends keys to
// ~/.ssh/authorized_keys, each once.
func userKeysScript(keys []string) string {
	var script strings.Builder
	script.WriteString("mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys")
	for _, k := range keys {
		// "--" and printf: an entry with options may start with any word,
		// even one that looks like a flag of grep or echo.
		q := shellQuoteMCP(k)
		script.WriteString(fmt.Sprintf(" && { grep -qxF -- %s ~/.ssh/authorized_keys || printf '%%s\\n' %s >> ~/.ssh/authorized_keys; }", q, q))
	}
	return script.String()
}

// knownHostsFile is the project's own known_hosts, next to the deploy key:
// the ssh command deploy_apply and provision hand back checks the VM's host
// key against it instead of turning the check off.
const knownHostsFile = "known_hosts"

// deployKeySSH finishes SSH access to a new VM at ip made with the agent's
// deploy key (deploy_apply, provision): it returns the ssh command that logs
// in as login with that key, checking the host key against the project's
// known_hosts (accept-new records it on the first connection), and makes
// that file trust hostKey for ip and nothing else for it (pinHostKey; an
// empty hostKey only drops an earlier VM's entry). warning is the sentence
// for the answer when the file could not be updated; the command works
// without it. A plain "ssh user@IP" tries ~/.ssh/id_* and is refused
// (dogfood 1.2.1).
func deployKeySSH(stateDir, login, ip, hostKey string) (command, warning string) {
	dataDir := config.DataDir(stateDir)
	keyPath := filepath.Join(dataDir, "id_ed25519")
	knownHosts := filepath.Join(dataDir, knownHostsFile)
	command = fmt.Sprintf("ssh -i %s -o UserKnownHostsFile=%s -o StrictHostKeyChecking=accept-new %s@%s", keyPath, knownHosts, login, ip)
	if err := pinHostKey(knownHosts, ip, hostKey); err != nil {
		// Without the file the ssh command still works (accept-new
		// creates it); a stale entry would make it refuse the new VM.
		warning = fmt.Sprintf(" WARNING: could not update %s (%v); if ssh reports a changed host key, remove the line for %s from it.", knownHosts, err, ip)
	}
	return command, warning
}

// pinHostKey makes the project's known_hosts trust key for host and nothing
// else for it: Cloud.ru reuses public IPs, so an entry for an earlier VM on
// the same address is dropped. An empty key only drops the old entries (the
// first ssh then records the new one: StrictHostKeyChecking=accept-new).
func pinHostKey(path, host, key string) error {
	// Two deploy_apply in one project (stdio and HTTP servers) would
	// otherwise read the same file and the last write drop the other's line.
	unlock, err := state.FileLock(path+".lock", 30*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	var keep []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if line == "" || knownHostsLineFor(line, host) {
				continue
			}
			keep = append(keep, line)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if key != "" {
		keep = append(keep, host+" "+key)
	}
	out := ""
	if len(keep) > 0 {
		out = strings.Join(keep, "\n") + "\n"
	}
	return os.WriteFile(path, []byte(out), 0o600)
}

// knownHostsLineFor reports whether a known_hosts line names host (plain or
// "[host]:22", alone or in a comma list).
func knownHostsLineFor(line, host string) bool {
	hosts, _, _ := strings.Cut(line, " ")
	for _, h := range strings.Split(hosts, ",") {
		if h == host || h == "["+host+"]:22" {
			return true
		}
	}
	return false
}

// shellQuoteMCP single-quotes s for safe interpolation into a shell command.
func shellQuoteMCP(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
