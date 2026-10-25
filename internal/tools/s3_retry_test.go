// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

// The S3 client retries a 429, a 5xx and a cut answer on its own clock:
// minio-go sleeps 200 ms, 400 ms, … up to 1 s between up to ten attempts,
// and nothing of evo's shortens it. In the test worlds that wait was the
// whole case: an S3 call on a 500 ran until the test's context expired
// (TestContract_APIStatusReason, 400 ms × 28 cases), and its reason went
// unchecked — the case was skipped as "minio retries on its own clock".
// CLAUDE.md §5: the worlds do not wait production intervals. One attempt
// here, and no sleep after it (minio's retry timer sleeps once more after
// its last attempt, up to the unit of 200 ms); the production client keeps
// minio's defaults.
func init() {
	minio.MaxRetry = 1
	minio.DefaultRetryUnit = 0
}

// Every S3 tool sends its failing request once: the same request again
// would be minio's retry and its sleep (weekly-race on Windows, run
// 40135541445: both timeouts of the probe stood in minio-go
// newRetryTimer). A different request after the failure is the tool's
// own read (s3_bucket_create asks whether the bucket exists).
func TestS3World_OneRequestPerFailure(t *testing.T) {
	rt := &contractRT{}
	reg := contractReg(t, rt)
	for _, tl := range reg.Tools() {
		if !strings.HasPrefix(tl.Name, "s3_") {
			continue
		}
		args := contractArgs(tl)
		if tl.Mutating {
			args["confirmed"] = true
		}
		for _, kind := range []string{"429", "500", "502", "html"} {
			rt.kind = kind
			rt.take()
			_, isErr, pan := contractCall(tl, args, contractCallLimit)
			reqs := rt.take()
			switch {
			case pan != "":
				t.Errorf("%s %s panicked", tl.Name, kind)
			case !isErr:
				t.Errorf("%s %s: not an error", tl.Name, kind)
			case len(reqs) == 0:
				t.Errorf("%s %s: no request", tl.Name, kind)
			case repeated(reqs) != "":
				t.Errorf("%s %s: %q sent twice — minio retried: %v", tl.Name, kind, repeated(reqs), reqs)
			}
		}
	}
}

// repeated is the first request of reqs sent more than once, or "".
func repeated(reqs []string) string {
	seen := map[string]bool{}
	for _, q := range reqs {
		if seen[q] {
			return q
		}
		seen[q] = true
	}
	return ""
}
