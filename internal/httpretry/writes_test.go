// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"context"
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
// not its writes, and a read is no write. A request that does not go
// through a Transport is not counted: the POST for a token of IAM
// (UserAgent alone, internal/auth) is no change of the cloud, and a call
// whose only POST it was has sent nothing («Риск», the conclusion on D107,
// Ут1 and Ут2).
func TestCountWrites_OfTheCallAlone(t *testing.T) {
	api := &http.Client{Transport: &Transport{Base: okTransport{}}}
	token := &http.Client{Transport: UserAgent(okTransport{})}
	send := func(ctx context.Context, c *http.Client, method string) {
		req, _ := http.NewRequestWithContext(ctx, method, "https://api.example/v1/things", strings.NewReader("{}"))
		resp, err := c.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		resp.Body.Close()
	}
	mine := CountWrites(context.Background())
	other := CountWrites(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			send(other, api, http.MethodPost)
			send(mine, api, http.MethodGet)
			send(mine, token, http.MethodPost)
		}()
	}
	wg.Wait()
	if n, counted := Writes(mine); !counted || n != 0 {
		t.Errorf("a call that only read, and asked for a token, sent %d writes (counted %v), want 0", n, counted)
	}
	if n, _ := Writes(other); n != 8 {
		t.Errorf("the other call sent %d writes, want 8", n)
	}
	send(mine, api, http.MethodDelete)
	if n, _ := Writes(mine); n != 1 {
		t.Errorf("after one write of its own the call counts %d", n)
	}
	if _, counted := Writes(context.Background()); counted {
		t.Error("a context that does not count says it does")
	}
}
