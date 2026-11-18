// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// shapedBody is a body of an answer as a connection gives it: data in
// reads of at most chunk bytes, then its end — the clean io.EOF, or err in
// the same read as the last bytes or in a read of its own, and io.EOF after
// that (a connection that broke once). It counts its Close.
type shapedBody struct {
	data    []byte
	chunk   int
	err     error
	ownRead bool
	off     int
	ended   bool
	closed  *int
}

// errClose is the error the Close of every shapedBody answers: a caller
// that gets its body given on gets that error too (Астра #2197).
var errClose = io.ErrClosedPipe

func (b *shapedBody) Read(p []byte) (int, error) {
	if b.off < len(b.data) {
		if len(p) > b.chunk {
			p = p[:b.chunk]
		}
		n := copy(p, b.data[b.off:])
		b.off += n
		if b.off == len(b.data) && b.err != nil && !b.ownRead && !b.ended {
			b.ended = true
			return n, b.err
		}
		return n, nil
	}
	if b.err != nil && !b.ended {
		b.ended = true
		return 0, b.err
	}
	return 0, io.EOF
}

func (b *shapedBody) Close() error {
	*b.closed++
	return errClose
}

// bodyShape is one input of the class: a size and the way the body ends.
type bodyShape struct {
	size    int
	err     error
	ownRead bool
}

func (s bodyShape) String() string {
	end := "clean end"
	if s.err != nil {
		end = fmt.Sprintf("%v with the bytes", s.err)
		if s.ownRead {
			end = fmt.Sprintf("%v in a read of its own", s.err)
		}
	}
	return fmt.Sprintf("%d bytes, %s", s.size, end)
}

// serviceBody is a 404 of a service (serviceNotFound) of exactly size
// bytes, or its first size bytes when the form is longer.
func serviceBody(size int) []byte {
	const head, tail = `{"code":5,"message":"resource not found","details":"`, `"}`
	pad := size - len(head) - len(tail)
	if pad < 0 {
		return []byte((head + tail)[:size])
	}
	return []byte(head + strings.Repeat("x", pad) + tail)
}

// errCut is an error of a read that is not one of io's.
var errCut = errors.New("connection reset by peer")

// shapesAround are the inputs of the class at a limit: an empty body, the
// limit -1, the limit and the limit +1, each ending clean, with an error in
// the read of its last bytes and with an error in a read of its own.
func shapesAround(limit int) []bodyShape {
	var shapes []bodyShape
	for _, size := range []int{0, limit - 1, limit, limit + 1} {
		if size < 0 {
			continue
		}
		shapes = append(shapes, bodyShape{size: size})
		for _, err := range []error{io.ErrUnexpectedEOF, errCut} {
			shapes = append(shapes, bodyShape{size, err, false}, bodyShape{size, err, true})
		}
	}
	return shapes
}

// sameAsTheAPI checks that what the caller reads from got is what it
// would read from the body the API gave: the bytes, the error the read
// ends with (nil for a clean end), and one Close of that body, with its
// error.
func sameAsTheAPI(t *testing.T, name string, s bodyShape, data []byte, got io.ReadCloser, closed *int) {
	t.Helper()
	wantBytes, wantErr := io.ReadAll(&shapedBody{data: data, chunk: 4096, err: s.err, ownRead: s.ownRead, closed: new(int)})
	b, err := io.ReadAll(got)
	if !bytes.Equal(b, wantBytes) || !errors.Is(err, wantErr) || (err == nil) != (wantErr == nil) {
		t.Errorf("%s, %v: the caller read %d bytes and %v; the API gave %d bytes and %v", name, s, len(b), err, len(wantBytes), wantErr)
	}
	if *closed != 0 {
		t.Errorf("%s, %v: the body was closed %d times before its caller closed it", name, s, *closed)
	}
	if err := got.Close(); !errors.Is(err, errClose) {
		t.Errorf("%s, %v: the caller's Close answered %v; the API's body answers %v", name, s, err, errClose)
	}
	if *closed != 1 {
		t.Errorf("%s, %v: the caller's Close closed the API's body %d times, want 1", name, s, *closed)
	}
}

// Every branch of the transport that reads a body before its caller does
// and promises to give it on gives the body the API gave: the same bytes,
// the same error at the end — a broken read does not end as a whole answer
// — and the caller's Close reaches it (class of #1746, external review
// #1769). The inputs are the edges of each read: an empty body, its limit
// ±1, an error with the last bytes and an error in a read of its own. A
// branch that replaces the answer (D63) has its own expectation below.
func TestBodyGivenOn_IsTheAPIs(t *testing.T) {
	t.Run("peek", func(t *testing.T) {
		const limit = 64
		for _, n := range []int64{0, 1, limit - 1, limit, limit + 1} {
			for _, s := range shapesAround(limit) {
				data := bytes.Repeat([]byte("a"), s.size)
				closed := new(int)
				head, body, err := peek(&shapedBody{data: data, chunk: 7, err: s.err, ownRead: s.ownRead, closed: closed}, n)
				if want := min(int(n), s.size); len(head) > want || !bytes.Equal(head, data[:len(head)]) {
					t.Errorf("peek %d, %v: head of %d bytes", n, s, len(head))
				}
				if err != nil && !errors.Is(err, s.err) {
					t.Errorf("peek %d, %v: error %v, not the body's", n, s, err)
				}
				sameAsTheAPI(t, fmt.Sprintf("peek %d", n), s, data, body, closed)
			}
		}
	})

	t.Run("service 404", func(t *testing.T) {
		for _, s := range shapesAround(maxServiceBody) {
			data := serviceBody(s.size)
			if s.size < maxServiceBody && !serviceNotFound(data) {
				continue // a cut form is replaced, below
			}
			closed := new(int)
			resp := &http.Response{StatusCode: 404, Header: http.Header{"Content-Type": {"application/json"}},
				Body: &shapedBody{data: data, chunk: 4096, err: s.err, ownRead: s.ownRead, closed: closed}}
			got := RouteNotFoundAsBadGateway(httptestRequest(), resp)
			if got.StatusCode != 404 {
				t.Errorf("service 404, %v: status %d", s, got.StatusCode)
				continue
			}
			sameAsTheAPI(t, "service 404", s, data, got.Body, closed)
		}
	})

	t.Run("recorded form", func(t *testing.T) {
		t.Setenv(EnvRecordAnswers, filepath.Join(t.TempDir(), "answers.jsonl"))
		for _, s := range shapesAround(maxRecordedBody + 1) {
			data := bytes.Repeat([]byte("a"), s.size)
			closed := new(int)
			resp := &http.Response{StatusCode: 200, Header: http.Header{},
				Body: &shapedBody{data: data, chunk: 32 << 10, err: s.err, ownRead: s.ownRead, closed: closed}}
			got := recorded(httptestRequest(), resp)
			sameAsTheAPI(t, "recorded form", s, data, got.Body, closed)
		}
	})

	// The layers together, as the clients get them: Transport (compute,
	// the services), UserAgent (the SDKs), Transport over UserAgent; the
	// forms recorded or not (Астра #2197: a read of UserAgent with the
	// recording off was not held).
	layers := map[string]func(http.RoundTripper) http.RoundTripper{
		"Transport":           func(rt http.RoundTripper) http.RoundTripper { return &Transport{Base: rt} },
		"UserAgent":           UserAgent,
		"Transport+UserAgent": func(rt http.RoundTripper) http.RoundTripper { return &Transport{Base: UserAgent(rt)} },
	}
	for _, record := range []bool{false, true} {
		for layer, wrap := range layers {
			t.Run(fmt.Sprintf("%s, recording %v", layer, record), func(t *testing.T) {
				if record {
					t.Setenv(EnvRecordAnswers, filepath.Join(t.TempDir(), "answers.jsonl"))
				} else {
					t.Setenv(EnvRecordAnswers, "")
				}
				for _, status := range []int{200, 404} {
					for _, limit := range []int{maxServiceBody, maxRecordedBody + 1} {
						for _, s := range shapesAround(limit) {
							data := serviceBody(s.size)
							if status == 404 && s.size < maxServiceBody && !serviceNotFound(data) {
								continue
							}
							closed := new(int)
							rt := rtFunc(func(r *http.Request) (*http.Response, error) {
								return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
									Body: &shapedBody{data: data, chunk: 32 << 10, err: s.err, ownRead: s.ownRead, closed: closed}, Request: r}, nil
							})
							resp, err := wrap(rt).RoundTrip(httptestRequest())
							if err != nil {
								t.Fatalf("%d, %v: %v", status, s, err)
							}
							if resp.StatusCode != status {
								t.Errorf("%d, %v: status %d", status, s, resp.StatusCode)
								resp.Body.Close()
								continue
							}
							sameAsTheAPI(t, fmt.Sprintf("%s %d", layer, status), s, data, resp.Body, closed)
						}
					}
				}
			})
		}
	}
}

// The branches that replace an answer — an HTML page for a 2xx, a 404
// the service did not write (D63) — give their own answer whatever the
// body was, and close the API's body: the caller's Close has nothing of
// it to close (Астра #2197: the HTML branch was not held).
func TestBodyGivenOn_ReplacedClosesTheAPIs(t *testing.T) {
	for _, s := range []bodyShape{{20, nil, false}, {20, io.ErrUnexpectedEOF, false}, {20, errCut, true}} {
		closed := new(int)
		resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body: &shapedBody{data: []byte("<html>gateway</html>"), chunk: 4096, err: s.err, ownRead: s.ownRead, closed: closed}}
		got := HTMLAsBadGateway(resp)
		b, err := io.ReadAll(got.Body)
		got.Body.Close()
		if got.StatusCode != http.StatusBadGateway || err != nil || !strings.Contains(string(b), "HTML page") {
			t.Errorf("html, %v: status %d, %q, %v; want the 502 of an HTML page", s, got.StatusCode, b, err)
		}
		if *closed != 1 {
			t.Errorf("html, %v: the API's body was closed %d times, want 1", s, *closed)
		}
	}
	for _, form := range []string{"", "404 page not found", `{"code":5,"message":"Not Found"}`} {
		for _, s := range []bodyShape{{len(form), nil, false}, {len(form), io.ErrUnexpectedEOF, false}, {len(form), errCut, true}} {
			closed := new(int)
			resp := &http.Response{StatusCode: 404, Header: http.Header{},
				Body: &shapedBody{data: []byte(form), chunk: 4096, err: s.err, ownRead: s.ownRead, closed: closed}}
			got := RouteNotFoundAsBadGateway(httptestRequest(), resp)
			b, err := io.ReadAll(got.Body)
			got.Body.Close()
			if got.StatusCode != http.StatusBadGateway || err != nil || !strings.Contains(string(b), routeNotFound) {
				t.Errorf("%q, %v: status %d, %q, %v; want the 502 of D63", form, s, got.StatusCode, b, err)
			}
			if *closed != 1 {
				t.Errorf("%q, %v: the API's body was closed %d times, want 1", form, s, *closed)
			}
		}
	}
}
