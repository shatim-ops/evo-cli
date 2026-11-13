// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type okTransport struct{}

func (okTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
}

// The writes of a call are counted by its own context: the writes of
// another call that runs beside it — two agents of one MCP server — are
// not its writes, and a read is no write. Every client of evo counts: the
// ones with their own loop of retries (UserAgent: Compute, S3) as well as
// Transport. The POST for a token (IAM, the OIDC of a cluster) is no
// change of the cloud: sent with NotAChange, it is not counted, and a call
// whose only POST it was has sent nothing («Риск», the conclusion on D107,
// Ут1 and Ут2; conclusion 187, У2).
func TestCountWrites_OfTheCallAlone(t *testing.T) {
	api := &http.Client{Transport: &Transport{Base: okTransport{}}}
	own := &http.Client{Transport: UserAgent(okTransport{})}
	send := func(ctx context.Context, c *http.Client, method string) {
		req, _ := http.NewRequestWithContext(ctx, method, "https://api.example/v1/things", strings.NewReader("{}"))
		resp, err := c.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		resp.Body.Close()
	}
	mine := CountWrites(context.Background(), nil)
	other := CountWrites(context.Background(), nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			send(other, api, http.MethodPost)
			send(other, own, http.MethodPut)
			send(mine, api, http.MethodGet)
			send(mine, own, http.MethodGet)
			send(NotAChange(mine), own, http.MethodPost)
			send(NotAChange(mine), api, http.MethodPost)
		}()
	}
	wg.Wait()
	if n, counted := Writes(mine); !counted || n != 0 {
		t.Errorf("a call that only read, and asked for a token, sent %d writes (counted %v), want 0", n, counted)
	}
	if n, _ := Writes(other); n != 16 {
		t.Errorf("the other call sent %d writes, want 16", n)
	}
	send(mine, own, http.MethodDelete)
	if n, _ := Writes(mine); n != 1 {
		t.Errorf("after one write of its own through a client with its own retries the call counts %d", n)
	}
	if _, counted := Writes(context.Background()); counted {
		t.Error("a context that does not count says it does")
	}
}

// The first write of a call is told before it goes out, once: the record
// of the call is marked «sent» then, so a process killed after it leaves
// the mark (conclusion 187, У2). A write that does not go over HTTP —
// kubectl, helm, a command over ssh — is told by NoteWrite at the point
// it is sent from.
func TestCountWrites_TheFirstWriteIsToldBeforeItGoesOut(t *testing.T) {
	told, sentBefore := 0, -1
	var ctx context.Context
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if sentBefore < 0 && req.Method != http.MethodGet {
			sentBefore = told
		}
		return okTransport{}.RoundTrip(req)
	})
	ctx = CountWrites(context.Background(), func() error { told++; return nil })
	c := &http.Client{Transport: UserAgent(base)}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPost} {
		req, _ := http.NewRequestWithContext(ctx, method, "https://api.example/v1/things", nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if err := NoteWrite(ctx); err != nil {
		t.Fatal(err)
	}
	if told != 1 || sentBefore != 1 {
		t.Errorf("the first write told %d times, %d times when it went out; want once, before it went", told, sentBefore)
	}
	if n, _ := Writes(ctx); n != 3 {
		t.Errorf("%d writes counted, want 3", n)
	}
	if err := NoteWrite(context.Background()); err != nil { // a context that does not count: nothing
		t.Fatal(err)
	}
}

// The mark of the first write that cannot be written keeps the write from
// going out: a record without its mark would say, after a kill, that
// nothing was sent.
func TestCountWrites_AMarkNotWrittenKeepsTheWriteBack(t *testing.T) {
	sent := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sent++
		return okTransport{}.RoundTrip(req)
	})
	ctx := CountWrites(context.Background(), func() error { return errors.New("the disk is full") })
	for _, c := range []*http.Client{{Transport: UserAgent(base)}, {Transport: &Transport{Base: base}}} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.example/v1/things", nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
			t.Error("a write went out with its mark not written")
		}
	}
	if err := NoteWrite(ctx); err == nil || sent != 0 {
		t.Errorf("sent %d, NoteWrite %v: want nothing sent and the error", sent, err)
	}
	if n, _ := Writes(ctx); n != 0 {
		t.Errorf("%d writes counted of none sent", n)
	}
}
