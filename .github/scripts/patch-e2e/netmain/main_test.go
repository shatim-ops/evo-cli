package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

const a = "00000000-0000-4000-8000-000000000001"
const b = "00000000-0000-4000-8000-000000000002"

type roundFunc func(*http.Request) (*http.Response, error)

func (f roundFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOriginalOperationReceiptOnly(t *testing.T) {
	fields, err := direct("vpcop", []byte(`{"id":"`+a+`","resourceId":"`+b+`"}`))
	if err != nil || fields["id"] != b || fields["operation"] != a {
		t.Fatal(fields, err)
	}
	for _, body := range []string{`{"name":"same"}`, `{"id":"` + a + `","resourceId":"not-id"}`, `null`} {
		if _, err := direct("vpcop", []byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestOnlyScopedDeleteAndNoSecondWrite(t *testing.T) {
	edits, calls := 0, 0
	g := &guard{s: spec{Host: "vpc.api.cloud.ru", Path: "/v1/vpcs/" + a, Method: "DELETE", Shape: "vpcop"},
		write: func(_ string, f obj) error { edits++; return nil }, base: roundFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 202, ContentLength: -1, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"` + b + `","resourceId":"` + a + `"}`))}, nil
		})}
	req, _ := http.NewRequest("DELETE", "https://vpc.api.cloud.ru/v1/vpcs/"+b, nil)
	if _, err := g.RoundTrip(req); err == nil || calls != 0 || edits != 0 {
		t.Fatal("foreign deletion")
	}
	req.URL.Path = "/v1/vpcs/" + a
	if _, err := g.RoundTrip(req); err != nil || calls != 1 || edits != 2 {
		t.Fatal(err, calls, edits)
	}
	if _, err := g.RoundTrip(req); err == nil || calls != 1 || edits != 2 {
		t.Fatal("delete replay")
	}
}
func TestNonPrivateOrForeignVMRejected(t *testing.T) {
	own := []any{obj{"interfaces": []any{obj{"type": "regular", "security_groups": []any{a}, "subnet_id": a}}}}
	extra := []any{obj{"interfaces": []any{obj{"type": "regular", "security_groups": []any{a}, "subnet_id": a, "attach_external_ip_id": b}}}}
	foreign := []any{obj{"interfaces": []any{obj{"type": "regular", "security_groups": []any{b}, "subnet_id": a}}}}
	if !privateVM(own) || privateVM(extra) || match(foreign, own) {
		t.Fatal("public or foreign interface passed")
	}
}
