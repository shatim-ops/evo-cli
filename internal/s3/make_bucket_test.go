// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

func init() {
	// One attempt and no sleep after it: the worlds do not wait
	// production intervals (CLAUDE.md §5).
	minio.MaxRetry = 1
	minio.DefaultRetryUnit = 0
}

// A bucket S3 refuses to create is the caller's only when S3 says so
// (BucketAlreadyOwnedByYou) or the caller's own list holds it. A HEAD
// answers for a bucket of another account with public read too: such a
// bucket was answered as «yours» («Риск», conclusion 130).
func TestMakeBucket_ExistedOnlyWhenTheBucketIsTheCallers(t *testing.T) {
	const list = `<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult><Owner><ID>o</ID></Owner><Buckets>%s</Buckets></ListAllMyBucketsResult>`
	for _, c := range []struct {
		name, putCode string
		listed        bool
		listStatus    int
		existed, fail bool
	}{
		{name: "created", existed: false},
		{name: "S3 says it is the caller's", putCode: "BucketAlreadyOwnedByYou", existed: true},
		{name: "the caller's list holds it", putCode: "BucketAlreadyExists", listed: true, existed: true},
		{name: "of another account, public read", putCode: "BucketAlreadyExists", fail: true},
		{name: "of another account, the list is refused", putCode: "BucketAlreadyExists", listStatus: http.StatusForbidden, fail: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				switch p := strings.Trim(r.URL.Path, "/"); {
				case r.Method == http.MethodGet && p == "":
					if c.listStatus != 0 {
						w.WriteHeader(c.listStatus)
						_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`)
						return
					}
					item := ""
					if c.listed {
						item = `<Bucket><Name>taken</Name><CreationDate>2026-10-20T10:00:00.000Z</CreationDate></Bucket>`
					}
					_, _ = io.WriteString(w, strings.Replace(list, "%s", item, 1))
				case r.Method == http.MethodPut && c.putCode != "":
					w.WriteHeader(http.StatusConflict)
					_, _ = io.WriteString(w, `<Error><Code>`+c.putCode+`</Code><Message>taken</Message></Error>`)
				case r.Method == http.MethodHead:
					// The bucket answers a HEAD whoever owns it.
				}
			}))
			defer srv.Close()
			cli, err := New(Config{Endpoint: srv.URL, AccessKey: "a", SecretKey: "s", Region: "ru-central-1"})
			if err != nil {
				t.Fatal(err)
			}
			existed, err := cli.MakeBucket(context.Background(), "taken", "")
			if existed != c.existed || (err != nil) != c.fail {
				t.Errorf("existed %v, err %v; want existed %v, an error %v", existed, err, c.existed, c.fail)
			}
			if c.listStatus != 0 {
				if _, err := cli.ListBuckets(context.Background()); !AccessDenied(err) {
					t.Errorf("the refused list is not AccessDenied: %v", err)
				}
			}
		})
	}
}
