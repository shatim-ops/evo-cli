// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package containerapp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func messages(lines []map[string]any) string {
	var s []string
	for _, l := range lines {
		s = append(s, fmt.Sprint(l["message"]))
	}
	return strings.Join(s, ",")
}

// Live run 18.10: ca_logs answered 0 lines after nginx served requests.
// Without pageSize other Cloud.ru lists answer an empty page with a new
// token (AR-4); a client that reads one page sees no lines.
func TestLogs_EmptyFirstPageWithoutPageSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("pageSize") == "":
			fmt.Fprint(w, `{"data":[],"nextPageToken":"x"}`)
		case q.Get("pageToken") == "":
			fmt.Fprint(w, `{"data":[{"timestamp":"2026-10-18T10:00:01Z","message":"a"}],"nextPageToken":"p2"}`)
		default:
			fmt.Fprint(w, `{"data":[{"timestamp":"2026-10-18T10:00:02Z","message":"b"}]}`)
		}
	}))
	defer srv.Close()

	got, err := New("tok", "proj", srv.URL).Logs(context.Background(), "app", false, 100)
	if err != nil {
		t.Fatal(err)
	}
	if messages(got.Lines) != "a,b" || got.Read != 2 || got.Cut {
		t.Fatalf("lines = %q, read %d, cut %v; want a,b", messages(got.Lines), got.Read, got.Cut)
	}
}

// The order of lines is not in the specification. Pages newest first: the
// last 3 lines by time are e,d,c of the first page and they come out
// oldest first. Before: the last 100 lines of the first page only, by
// position.
func TestLogs_LastByTimeAcrossPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("projectId") != "proj" {
			t.Errorf("projectId = %q", r.URL.Query().Get("projectId"))
		}
		if r.URL.Path != "/v2/containers/app/logs" {
			t.Errorf("path = %s", r.URL.Path)
		}
		switch r.URL.Query().Get("pageToken") {
		case "":
			fmt.Fprint(w, `{"data":[
				{"timestamp":"2026-10-18T10:00:05Z","message":"e"},
				{"timestamp":"2026-10-18T10:00:04.5Z","message":"d"},
				{"timestamp":"2026-10-18T10:00:03Z","message":"c"}],"nextPageToken":"p2"}`)
		case "p2":
			fmt.Fprint(w, `{"data":[
				{"timestamp":"2026-10-18T10:00:02Z","message":"b"},
				{"timestamp":"2026-10-18T10:00:01Z","message":"a"}],"nextPageToken":""}`)
		default:
			t.Errorf("unexpected token %q", r.URL.Query().Get("pageToken"))
		}
	}))
	defer srv.Close()

	got, err := New("tok", "proj", srv.URL).Logs(context.Background(), "app", false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if messages(got.Lines) != "c,d,e" || got.Read != 5 {
		t.Fatalf("lines = %q, read %d; want c,d,e of 5", messages(got.Lines), got.Read)
	}
}

// A fresh token on every page (as Artifact Registry answers, AR-4) with the
// token ignored: the same page again ends the read.
func TestLogs_TokenIgnored(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		fmt.Fprintf(w, `{"data":[{"timestamp":"2026-10-18T10:00:01Z","message":"a"}],"nextPageToken":"t%d"}`, n)
	}))
	defer srv.Close()

	got, err := New("tok", "proj", srv.URL).Logs(context.Background(), "app", false, 100)
	if err != nil {
		t.Fatal(err)
	}
	if messages(got.Lines) != "a" || got.Cut {
		t.Fatalf("lines = %q, cut %v; want a once", messages(got.Lines), got.Cut)
	}
	if c := calls.Load(); c != 2 {
		t.Fatalf("%d requests, want 2: the repeated page ends the read", c)
	}
}

// An empty page with a new token ends the read too.
func TestLogs_EmptyPageEnds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			fmt.Fprint(w, `{"data":[{"message":"a"}],"nextPageToken":"t1"}`)
			return
		}
		fmt.Fprintf(w, `{"data":[],"nextPageToken":"t%d"}`, n)
	}))
	defer srv.Close()

	got, err := New("tok", "proj", srv.URL).Logs(context.Background(), "app", false, 100)
	if err != nil {
		t.Fatal(err)
	}
	if messages(got.Lines) != "a" || calls.Load() != 2 {
		t.Fatalf("lines = %q after %d requests", messages(got.Lines), calls.Load())
	}
}

// A log that never ends: the read stops at logsMaxPages and says so, it
// does not fail and does not run for minutes.
func TestLogs_CutPastMaxPages(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		fmt.Fprintf(w, `{"data":[{"timestamp":"2026-10-18T10:00:00Z","message":"m%d"}],"nextPageToken":"t%d"}`, n, n)
	}))
	defer srv.Close()

	got, err := New("tok", "proj", srv.URL).Logs(context.Background(), "app", false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Cut || got.Read != logsMaxPages || int(calls.Load()) != logsMaxPages {
		t.Fatalf("cut %v, read %d, %d requests; want cut after %d", got.Cut, got.Read, calls.Load(), logsMaxPages)
	}
	// equal times keep the order of the API: the last two read
	if want := fmt.Sprintf("m%d,m%d", logsMaxPages-1, logsMaxPages); messages(got.Lines) != want {
		t.Fatalf("lines = %q, want %q", messages(got.Lines), want)
	}
}

// System logs: no pagination in the specification, newest first in the
// console (concepts__logging). Before: the last 100 by position, that is
// the oldest events when the API answers newest first.
func TestLogs_SystemByLastTimestamp(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v2/containers/app/systemLogs" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if q := r.URL.Query(); q.Get("pageSize") != "" || q.Get("pageToken") != "" {
			t.Errorf("query = %v: systemLogs has no pagination", q)
		}
		fmt.Fprint(w, `{"data":[
			{"lastTimestamp":"2026-10-18T10:00:03Z","message":"scaled"},
			{"firstTimestamp":"2026-10-18T10:00:02Z","message":"started"},
			{"lastTimestamp":"2026-10-18T10:00:01Z","message":"pulled"}],"nextPageToken":"x"}`)
	}))
	defer srv.Close()

	got, err := New("tok", "proj", srv.URL).Logs(context.Background(), "app", true, 2)
	if err != nil {
		t.Fatal(err)
	}
	if messages(got.Lines) != "started,scaled" || got.Read != 3 || calls.Load() != 1 {
		t.Fatalf("lines = %q, read %d, %d requests", messages(got.Lines), got.Read, calls.Load())
	}
}

func TestLogs_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(499)
		fmt.Fprint(w, `{"message":"Serverless with name is not found"}`)
	}))
	defer srv.Close()

	_, err := New("tok", "proj", srv.URL).Logs(context.Background(), "app", false, 100)
	if err == nil || !strings.Contains(err.Error(), "499") {
		t.Fatalf("err = %v", err)
	}
}
