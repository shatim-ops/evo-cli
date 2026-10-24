// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// W6 (G13-3): s3_bucket_delete of a bucket with objects asked for consent,
// and S3 refused after it (BucketNotEmpty). The plan lists the bucket
// before the question: one with objects is USAGE naming the way (s3_rm),
// with confirmed or without, and no DELETE goes; an empty one is the
// preview.
func TestS3BucketDelete_NotEmptyBeforeTheQuestion(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		f := &apiFake{own: liveWorld} // the bucket holds one object
		m, text, isErr := callNamed(t, apiFakeS3Registry(t, f), "s3_bucket_delete", map[string]any{"bucket": liveName, "confirmed": confirmed})
		if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, "not empty") || !strings.Contains(text, "s3_rm") {
			t.Errorf("a bucket with objects, confirmed=%v: %s", confirmed, text)
		}
		for _, c := range f.calls {
			if strings.HasPrefix(c, "DELETE") {
				t.Errorf("confirmed=%v: %s sent for a bucket with objects", confirmed, c)
			}
		}
	}
	empty := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`+
			`<Name>`+liveName+`</Name><KeyCount>0</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>`)
		return true
	}}
	if m, text, _ := callNamed(t, apiFakeS3Registry(t, empty), "s3_bucket_delete", map[string]any{"bucket": liveName}); m["status"] != "confirmation_required" {
		t.Errorf("an empty bucket: %s", text)
	}
}
