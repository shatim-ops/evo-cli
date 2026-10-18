// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pager

import (
	"errors"
	"strconv"
	"strings"
	"testing"
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

// A server that answers the same token again would loop forever.
func TestAll_StopsOnRepeatedToken(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestAll_ErrorPastMaxPages(t *testing.T) {
	calls := 0
	err := All("things", func(string) (string, error) {
		calls++
		return strconv.Itoa(calls), nil // a new token every time
	})
	if err == nil || !strings.Contains(err.Error(), "things: more than") {
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
