// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
)

// EnvRecordAnswers names a file each answer of the API that came over the
// network appends its form to, one JSON line: the request without values,
// the status and the paths of the body with the kind of each value (release
// plan 1.9, «Шов записи в транспорте»; step 3 of «Миры из живых ответов»).
// cloud-e2e sets it; the catalogue of live answers is made from the file
// (internal/cli/testdata/platform-answers.json). Unset: nothing is written.
const EnvRecordAnswers = "EVO_RECORD_ANSWERS"

// maxRecordedBody is how much of a body the form is read from: a longer
// one is recorded as "too-large", without paths.
const maxRecordedBody = 1 << 20

// recordedKey carries, on a request a Transport sends, whether a layer
// under it recorded the answer: the UserAgent of the compute client records
// it as the API gave it — before it turns an HTML page into a 502 — and
// the Transport over it then does not record it again.
type recordedKey struct{}

// recordMu keeps the lines of the goroutines of one evo whole.
var recordMu sync.Mutex

// recorded is resp after its form went to the file of EnvRecordAnswers;
// resp itself when the variable is unset. The body the caller reads is the
// API's, whole. A file that cannot be written changes nothing of the call.
// Its first maxRecordedBody bytes are read before the caller gets them: a
// download (an object of S3) starts after that read («Риск», conclusion
// 206, remark 2). The host and the segments of the path that are not ids
// stay in the file (requestForm): it is not to be published (condition У2).
func recorded(req *http.Request, resp *http.Response) *http.Response {
	path := os.Getenv(EnvRecordAnswers)
	if path == "" || resp == nil {
		return resp
	}
	var head []byte
	if resp.Body != nil {
		var err error
		head, err = io.ReadAll(io.LimitReader(resp.Body, maxRecordedBody+1))
		rest := io.Reader(resp.Body)
		if err != nil {
			// The caller gets the bytes read and then the same error, not
			// an end that reads as the whole answer (#1746); a broken body
			// has no form to record.
			rest = brokenBody{err}
		}
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(head), rest), resp.Body}
		if err != nil {
			return resp
		}
	}
	line := map[string]any{"request": req.Method + " " + requestForm(req), "status": resp.StatusCode}
	paths := map[string]string{}
	switch {
	case len(bytes.TrimSpace(head)) == 0:
		line["form"] = "empty"
	case len(head) > maxRecordedBody:
		line["form"] = "too-large"
	default:
		if kinds, _, err := AnswerShape(head); err == nil {
			line["form"] = "json"
			for p, k := range kinds {
				paths[keysForm(p)] = k
			}
		} else {
			line["form"] = "not-json"
		}
	}
	line["paths"] = paths
	b, err := json.Marshal(line)
	if err != nil {
		return resp
	}
	recordMu.Lock()
	defer recordMu.Unlock()
	// The path is the one the variable names, not a file a clone brings;
	// a link or another file that is not regular in its place is still
	// not written through (R-67).
	if st, err := os.Lstat(path); err == nil && !st.Mode().IsRegular() {
		return resp
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return resp
	}
	defer f.Close()
	// The mode of OpenFile is of a file it creates: a file already there
	// with a wider one gets 0600 before a line goes in, or no line (#1747).
	if !private(f) {
		return resp
	}
	_, _ = f.Write(append(b, '\n'))
	return resp
}

// private reports whether f is readable by its owner only, making it so
// when it is not.
func private(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	if st.Mode().Perm() == 0o600 {
		return true
	}
	return f.Chmod(0o600) == nil
}

// brokenBody reads as the error the read of a body ended with.
type brokenBody struct{ err error }

func (b brokenBody) Read([]byte) (int, error) { return 0, b.err }

// recordBelow is req that a layer under the caller may record (recordedBy),
// with what it says of it.
func recordBelow(req *http.Request) (*http.Request, *bool) {
	done := new(bool)
	if os.Getenv(EnvRecordAnswers) == "" {
		return req, done
	}
	return req.WithContext(context.WithValue(req.Context(), recordedKey{}, done)), done
}

// recordedBy records the answer to req at the lowest layer that sees it:
// the layer over it is told (recordBelow).
func recordedBy(req *http.Request, resp *http.Response) *http.Response {
	if done, ok := req.Context().Value(recordedKey{}).(*bool); ok {
		if *done {
			return resp
		}
		*done = true
	}
	return recorded(req, resp)
}

// idSegment is a segment of a path that is an id: a UUID or a number.
var idSegment = regexp.MustCompile(`^(?:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9]+)$`)

// requestForm is the address of req without the query and with {id} for
// each segment of its path that is an id. Another segment — the name of a
// bucket, of a DNS zone — stays: the generator of the catalogue keeps only
// the requests of the declared templates (scripts/api-contract.json).
func requestForm(req *http.Request) string {
	segs := strings.Split(req.URL.EscapedPath(), "/")
	for i, s := range segs {
		if idSegment.MatchString(s) {
			segs[i] = "{id}"
		}
	}
	return "https://" + req.URL.Host + strings.Join(segs, "/")
}

// fieldKey is the form of a key of an object that names a field; any other
// key (a label, a tag, a variable of an environment) is a value.
var fieldKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// keysForm is a path of AnswerShape with "*" for each key that does not
// name a field.
func keysForm(path string) string {
	parts := strings.Split(path, ".")
	for i, p := range parts {
		if p != "[]" && !fieldKey.MatchString(p) {
			parts[i] = "*"
		}
	}
	return strings.Join(parts, ".")
}

// uuidValue is a string value that is a UUID.
var uuidValue = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// AnswerShape is the form of a JSON body: each leaf path ("a.[].b") with
// the kind of its values — "uuid", "name" (any other string), "number",
// "bool", "null", "object" or "array" for an empty one — and the strings
// seen at each path. No value leaves this function but through strs: the
// file of EnvRecordAnswers takes the kinds only, and the tests of the
// worlds compare strs with the catalogue and never print them.
func AnswerShape(body []byte) (map[string]string, map[string][]string, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, nil, err
	}
	kinds, strs := map[string]string{}, map[string][]string{}
	var walk func(v any, path string)
	walk = func(v any, path string) {
		join := func(k string) string {
			if path == "" {
				return k
			}
			return path + "." + k
		}
		switch x := v.(type) {
		case map[string]any:
			if len(x) == 0 {
				kinds[path] = "object"
			}
			for k, e := range x {
				walk(e, join(k))
			}
		case []any:
			if len(x) == 0 {
				kinds[path] = "array"
			}
			for _, e := range x {
				walk(e, join("[]"))
			}
		case string:
			kind := "name"
			if uuidValue.MatchString(x) {
				kind = "uuid"
			}
			kinds[path] = kind
			strs[path] = append(strs[path], x)
		case float64:
			kinds[path] = "number"
		case bool:
			kinds[path] = "bool"
		case nil:
			kinds[path] = "null"
		}
	}
	walk(v, "")
	return kinds, strs, nil
}
