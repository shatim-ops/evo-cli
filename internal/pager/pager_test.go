// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pager

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func TestAll_FollowsTokenUntilEmpty(t *testing.T) {
	var got []string
	err := All("things", func(token string) (string, error) {
		got = append(got, token)
		switch token {
		case "":
			return "p2", nil
		case "p2":
			return "p3", nil
		}
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != ",p2,p3" {
		t.Fatalf("tokens sent: %q", got)
	}
}

// A server that answers the same token again would loop forever, and
// where its list ends is not known: an error after three requests, not a
// list that ends there (break r4, R4-6; it was the end of the list before).
func TestAll_RepeatedTokenIsError(t *testing.T) {
	calls := 0
	err := All("things", func(token string) (string, error) {
		calls++
		if token == "" {
			return "a", nil
		}
		if token == "a" {
			return "b", nil
		}
		return "a", nil // back to a token already followed
	})
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError ||
		!strings.HasPrefix(ce.Message, "things: ") || !strings.Contains(ce.Message, `"a"`) {
		t.Fatalf("err = %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

// StopOnRepeat keeps the end on a repeated token for a tail read that
// reports a cut itself (containerapp.Logs).
func TestUpto_StopOnRepeat(t *testing.T) {
	calls := 0
	more, err := Upto(10, func(string) (string, error) {
		calls++
		return "same", nil
	}, StopOnRepeat)
	if err != nil || more || calls != 2 {
		t.Fatalf("more = %v, err = %v, calls = %d", more, err, calls)
	}
}

// The page's own error keeps its text: no list name in front of it.
func TestAll_FetchErrorNotRenamed(t *testing.T) {
	boom := clierrors.New(clierrors.CodeNotFound, "gone", "")
	err := All("things", func(string) (string, error) { return "", boom })
	if err != boom {
		t.Fatalf("err = %v", err)
	}
}

func TestAll_ErrorPastMaxPages(t *testing.T) {
	calls := 0
	err := All("things", func(string) (string, error) {
		calls++
		return strconv.Itoa(calls), nil // a new token every time
	})
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError || !strings.HasPrefix(ce.Message, "things: ") || !strings.Contains(ce.Message, strconv.Itoa(MaxPages)) {
		t.Fatalf("err = %v", err)
	}
	if calls != MaxPages {
		t.Fatalf("calls = %d, want %d", calls, MaxPages)
	}
}

func TestAll_FetchErrorStops(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	err := All("things", func(string) (string, error) {
		calls++
		if calls == 2 {
			return "", boom
		}
		return "next", nil
	})
	if !errors.Is(err, boom) || calls != 2 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestUpto_ReportsMore(t *testing.T) {
	calls := 0
	more, err := Upto(3, func(string) (string, error) {
		calls++
		return strconv.Itoa(calls), nil // a new token every time
	})
	if err != nil || !more || calls != 3 {
		t.Fatalf("more = %v, err = %v, calls = %d", more, err, calls)
	}
	more, err = Upto(3, func(token string) (string, error) {
		if token == "" {
			return "a", nil
		}
		return "", nil
	})
	if err != nil || more {
		t.Fatalf("complete list: more = %v, err = %v", more, err)
	}
}
