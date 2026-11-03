// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package vmstage runs a long stage of deploy on the VM — the Docker
// install, docker compose up — apart from the SSH session that starts it,
// and reads how it goes (D69, variant C, owner's decision 03.11). The
// session can end, by the budget of an MCP call or a lost connection, and
// the stage goes on; the next call reads it instead of starting it again.
package vmstage

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Stage is what the VM says of a stage.
type Stage struct {
	// State: "none" (never started), "running", "done", "failed", or
	// "lost" — running, but no process holds its lock: the VM rebooted,
	// the process was killed.
	State string
	// Exit is the stage's exit code once it is done or failed.
	Exit int
	// Input is what the stage was started for (a hash of its script and
	// what it reads): a stage done for another input is not this one.
	Input string
	// LogTail is the end of the stage's output.
	LogTail string
}

// Runner starts a stage on the VM and reads it.
type Runner interface {
	// Start runs the stage name detached on the VM unless it is running:
	// started is false then, and nothing else happens (one stage of a
	// name at a time).
	Start(ctx context.Context, name, input, script string) (started bool, err error)
	// Read says how the stage name goes.
	Read(ctx context.Context, name string) (Stage, error)
}

// Execer runs a command on the VM and returns its output: deploy.Deployer.
type Execer interface {
	Exec(ctx context.Context, cmd string) (string, error)
}

//go:embed stage.sh
var stageScript string

// Over is the Runner of the VM e reaches over SSH: each call puts the
// stage script in ~/.evo/stage and runs it there.
func Over(e Execer) Runner { return sshRunner{e} }

type sshRunner struct{ e Execer }

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// command is the shell command that runs the stage script with args.
func command(args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + a + "'"
	}
	// Written aside and moved in: a stage that runs reads the script
	// while the next call puts it in again.
	return `umask 077 && mkdir -p "$HOME/.evo/stage" && printf %s ` + base64.StdEncoding.EncodeToString([]byte(stageScript)) +
		` | base64 -d >"$HOME/.evo/stage/evo-stage.sh.$$" && mv -f "$HOME/.evo/stage/evo-stage.sh.$$" "$HOME/.evo/stage/evo-stage.sh" && sh "$HOME/.evo/stage/evo-stage.sh" ` + strings.Join(quoted, " ")
}

func checkName(name, input string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("stage name %q: lower-case letters, digits and dashes", name)
	}
	if strings.ContainsAny(input, "'\n") {
		return fmt.Errorf("stage input of %s: no quotes or line breaks", name)
	}
	return nil
}

func (r sshRunner) Start(ctx context.Context, name, input, script string) (bool, error) {
	if err := checkName(name, input); err != nil {
		return false, err
	}
	out, err := r.e.Exec(ctx, command("start", name, input, base64.StdEncoding.EncodeToString([]byte(script))))
	if err != nil {
		return false, fmt.Errorf(i18n.T("start the stage %s on the VM: %w", "запуск этапа %s на ВМ: %w"), name, err)
	}
	switch strings.TrimSpace(out) {
	case "started=true":
		return true, nil
	case "started=false":
		return false, nil
	}
	return false, fmt.Errorf("start the stage %s on the VM: unexpected answer %q", name, strings.TrimSpace(out))
}

func (r sshRunner) Read(ctx context.Context, name string) (Stage, error) {
	if err := checkName(name, ""); err != nil {
		return Stage{}, err
	}
	out, err := r.e.Exec(ctx, command("status", name))
	if err != nil {
		return Stage{}, fmt.Errorf(i18n.T("read the stage %s on the VM: %w", "чтение этапа %s на ВМ: %w"), name, err)
	}
	return parse(out)
}

// parse reads the status the stage script prints.
func parse(out string) (Stage, error) {
	head, log, _ := strings.Cut(out, "--log--\n")
	var st Stage
	for _, line := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "state":
			st.State = v
		case "exit":
			st.Exit, _ = strconv.Atoi(v)
		case "input":
			st.Input = v
		}
	}
	switch st.State {
	case "none", "running", "done", "failed", "lost":
	default:
		return Stage{}, fmt.Errorf("the stage's status on the VM is not read: %q", out)
	}
	st.LogTail = strings.TrimRight(log, "\n")
	return st, nil
}
