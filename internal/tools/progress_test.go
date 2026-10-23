// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"
)

// The lines of an MCP client's progress (WithProgress) are the lines a
// printer shows without a terminal, one at a time, trimmed, blank ones
// dropped, a line sent once it ends.
func TestProgress_Lines(t *testing.T) {
	var got []string
	pp, watched := progressOf(WithProgress(context.Background(), func(l string) { got = append(got, l) }))
	if !watched {
		t.Fatal("a client that asked for the progress is not watching")
	}
	pp.Step(0, 0, "Creating VM web…")
	pp.StepDone("Request accepted")
	pp.Progress("redrawn on a terminal only")
	if want := "==> Creating VM web…\nRequest accepted"; strings.Join(got, "\n") != want {
		t.Errorf("lines %q, want %q", got, want)
	}
}

// A tool a tool calls (callTool: the steps of a stack) shows no progress of
// its own on either surface: in the CLI it gets no Caller, over MCP no
// progress either — the outer tool's progress is the call's.
func TestProgress_StepShowsNone(t *testing.T) {
	sent := 0
	ctx := WithProgress(context.Background(), func(string) { sent++ })
	ctx = WithCaller(ctx, &Caller{Progress: silent})
	if _, watched := progressOf(stepOf(ctx)); watched {
		t.Error("a step is watched")
	}
	pp, _ := progressOf(stepOf(ctx))
	pp.Step(0, 0, "step")
	if sent != 0 {
		t.Errorf("a step sent %d lines", sent)
	}
}
