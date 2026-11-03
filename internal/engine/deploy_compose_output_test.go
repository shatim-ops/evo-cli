// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/vmstage"
)

// Release plan 1.6 (#558; Fable on the Grok wave of rc.1, low): with --json
// the output of docker compose up went nowhere, and its failure said only
// «docker compose up failed». The error carries the last lines of that
// output in its details (output) — the workload's words, which the tool
// marks untrusted — in every mode; the lines before them are not kept.
func TestDeploy_ComposeUpFailureCarriesItsLastLines(t *testing.T) {
	for _, mode := range []output.Mode{output.ModeJSON, output.ModePlain} {
		dep := newMockDeployer()
		upCmd := "cd /opt/app && docker compose -f docker-compose.yml up -d"
		var lines []string
		for i := 1; i <= composeTailLines+5; i++ {
			lines = append(lines, fmt.Sprintf("line %d", i))
		}
		lines = append(lines, "Error response from daemon: pull access denied for app")
		dep.streamOut = func(cmd string) string {
			if cmd == upCmd {
				return strings.Join(lines, "\n") + "\n"
			}
			return ""
		}
		dep.streamErrs = map[string][]error{upCmd: {stderrors.New("Process exited with status 1")}}
		eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
		var buf bytes.Buffer
		eng.Printer = output.NewWithWriter(&buf, mode, false)
		err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
		var ce *clerrors.CLIError
		if !stderrors.As(err, &ce) {
			t.Fatalf("mode %v: Deploy = %v", mode, err)
		}
		out, _ := ce.Details["output"].(string)
		if !strings.Contains(out, "pull access denied") || strings.Contains(out, "line 1\n") || strings.Count(out, "\n") > composeTailLines {
			t.Errorf("mode %v: output = %q, want the last %d lines", mode, out, composeTailLines)
		}
	}
}

// A progress bar rewrites one line with \r and no \n for the whole pull:
// the unfinished line is kept bounded, and a cut keeps UTF-8 whole (review
// of #566).
func TestTailWriter_UnfinishedLineStaysBounded(t *testing.T) {
	w := newTailWriter(composeTailLines)
	for i := 0; i < 10000; i++ {
		_, _ = w.Write([]byte("\rзагрузка ▓▓▓▓▓▓▓▓ 42%"))
	}
	w.mu.Lock()
	n := len(w.part)
	w.mu.Unlock()
	if n > 2*tailLineMax {
		t.Errorf("unfinished line holds %d bytes, want at most %d", n, 2*tailLineMax)
	}
	if s := w.String(); !utf8.ValidString(s) {
		t.Errorf("the tail is not valid UTF-8: %q", s)
	}
	_, _ = w.Write([]byte("\n" + strings.Repeat("ё", tailLineMax) + "\n"))
	if s := w.String(); !utf8.ValidString(s) {
		t.Errorf("a long line cut through a rune: %q", s[len(s)-10:])
	}
}

// logStages is a VM whose compose-up stage fails with log as its output;
// other stages are done at once.
type logStages struct {
	log    string
	inputs sync.Map
}

func (s *logStages) Start(_ context.Context, name, input, _ string) (bool, error) {
	s.inputs.Store(name, input)
	return true, nil
}

func (s *logStages) Read(_ context.Context, name string) (vmstage.Stage, error) {
	in, ok := s.inputs.Load(name)
	switch {
	case !ok:
		return vmstage.Stage{State: "none"}, nil
	case name != "compose-up":
		return vmstage.Stage{State: "done", Input: in.(string)}, nil
	}
	return vmstage.Stage{State: "failed", Exit: 1, Input: in.(string), LogTail: s.log}, nil
}

// The log of a stage on the VM is the output of docker compose up as the
// stream of the session was: a progress bar rewritten with \r is one line
// of what a terminal shows, bounded, and UTF-8 stays whole (#566; the
// stage of D69 reads the log, not the stream).
func TestDeploy_StageLogTailIsBoundedLikeTheStream(t *testing.T) {
	log := "Error response from daemon: pull access denied\n" + strings.Repeat("\rзагрузка ▓▓▓▓▓▓▓▓ 42%", 5000)
	eng, dir := newTestEngine(t, &mockCloudAPI{}, newMockDeployer())
	eng.Stages = &logStages{log: log}
	err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) {
		t.Fatalf("Deploy = %v", err)
	}
	out, _ := ce.Details["output"].(string)
	if len(out) > 2*tailLineMax+100 || !utf8.ValidString(out) || !strings.Contains(out, "pull access denied") || strings.Count(out, "\r") > 0 {
		t.Errorf("output of %d bytes, want the lines a terminal shows, bounded: %q…", len(out), firstBytes(out, 200))
	}
}
