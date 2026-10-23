// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The fake world of TestParity_JSONEqualsTool: the world of the previews
// (withParityWorld) plus what the answers read and write beyond them — one
// fixture per API host, answering every call of the tools with a command
// with the least that makes the call succeed. Every ID is fake
// (00000000-…), every address in 192.0.2.0/24 or 203.0.113.0/24, every
// domain example.com.

// jsonOp is an async operation that is already done, as every API of the
// fake answers a create or a delete.
const jsonOp = `{"id":"` + parityVM + `","resource_id":"` + parityVM + `","resourceId":"` + parityVM + `","done":true}`

// jsonFake is the state the answers change: a VM deleted is gone after, a
// floating IP attached is in use.
type jsonFake struct {
	mu       sync.Mutex
	vmGone   bool
	fipBound bool     // the floating IP was attached to the VM
	writes   []string // every request that is not a read, with its body
}

func (f *jsonFake) reset() {
	f.mu.Lock()
	f.vmGone, f.fipBound, f.writes = false, false, nil
	f.mu.Unlock()
}

// takeWrites returns the writes since the last reset, sorted: calls made in
// parallel come in any order.
func (f *jsonFake) takeWrites() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.writes...)
	sort.Strings(out)
	return out
}

// record notes a write: method, URL and body (put back for the fixture).
func (f *jsonFake) record(r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return
	}
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	f.mu.Lock()
	f.writes = append(f.writes, r.Method+" "+r.URL.Host+r.URL.Path+"?"+r.URL.RawQuery+" "+string(body))
	f.mu.Unlock()
}

func (f *jsonFake) gone() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vmGone
}

// answer is the fixture of request r, nil for parityRT to answer.
func (f *jsonFake) answer(r *http.Request) *http.Response {
	w := httptest.NewRecorder()
	get := r.Method == http.MethodGet
	write := func(body string) *http.Response {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
		return w.Result()
	}
	p := r.URL.Path
	if r.Method == http.MethodPost && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		// The OIDC token of a kubeconfig (k8s kubectl, apply, helm).
		return write(`{"access_token":"fake-kube-token"}`)
	}
	switch r.URL.Host {
	case "compute.api.cloud.ru":
		switch {
		case p == "/api/v1/vms/"+parityVM && get && !f.gone():
			// The VM of parityVM with its primary interface, ready: the
			// floating IP of provision and deploy goes there.
			return write(`{"id":"` + parityVM + `","name":"web","state":"running","interfaces":[{"id":"` + parityNIC +
				`","type":"regular","ip_address":"192.0.2.5","primary":true,"state":"created"}]}`)
		case p == "/api/v1/vms/"+parityVM && r.Method == http.MethodDelete:
			f.mu.Lock()
			f.vmGone = true
			f.mu.Unlock()
			return write(`{}`)
		case p == "/api/v1/vms/"+parityVM && get && f.gone():
			w.WriteHeader(404)
			return write(`{"message":"not found"}`)
		case p == "/api/v1.1/vms" && r.Method == http.MethodPost:
			return write(`[{"id":"` + parityVM + `","name":"web","state":"running"}]`)
		case p == "/api/v1/images" && r.Method == http.MethodPost:
			return write(`{"id":"` + parityVM + `","name":"golden"}`)
		case p == "/api/v1/floating-ips" && r.Method == http.MethodPost:
			return write(`{"id":"` + parityVM + `","ip_address":"203.0.113.10","state":"available"}`)
		case p == "/api/v1/floating-ips/"+parityVM && r.Method == http.MethodPut:
			f.mu.Lock()
			f.fipBound = true
			f.mu.Unlock()
			return write(`{"id":"` + parityVM + `","ip_address":"203.0.113.10","state":"in_use"}`)
		case p == "/api/v1/floating-ips/"+parityVM && get:
			f.mu.Lock()
			bound := f.fipBound
			f.mu.Unlock()
			if bound {
				return write(`{"id":"` + parityVM + `","ip_address":"203.0.113.10","state":"in_use","vm":{"id":"` + parityVM + `"}}`)
			}
			return write(`{"id":"` + parityVM + `","ip_address":"203.0.113.10","state":"available"}`)
		case strings.HasPrefix(p, "/api/v1/security-groups/") && strings.HasSuffix(p, "/rules") && r.Method == http.MethodPost:
			return write(`{"id":"` + parityVM + `","direction":"ingress","ip_protocol":"tcp"}`)
		}
	case "s3.cloud.ru":
		return s3Parity(r)
	case "dns.api.cloud.ru":
		switch {
		case p == "/v1/publicZones" && get:
			return write(`{"zones":[{"id":"` + parityVM + `","name":"example","domain":"example.com.","state":"ok"}]}`)
		case p == "/v1/publicZones/"+parityVM && get:
			return write(`{"id":"` + parityVM + `","name":"example","domain":"example.com.","state":"ok"}`)
		case p == "/v1/privateZones" && get:
			return write(`{"zones":[]}`)
		case strings.HasSuffix(p, "RecordsSole") && get:
			return write(`{"records":[]}`)
		case strings.Contains(p, "RecordsSole/") && get:
			return write(`{"id":"` + parityVM + `","name":"www","type":"A","values":["192.0.2.10"]}`)
		case strings.HasPrefix(p, "/v1/operations/") || !get:
			return write(jsonOp)
		}
	case "mk8s.api.cloud.ru":
		switch {
		case p == "/v3/productConfiguration":
			return write(`{"defaultZone":"` + pvZone + `","defaultChannel":"stable","defaultDiskType":"SSD",` +
				`"zones":[{"zoneId":"` + pvZone + `","name":"ru.AZ-1","availableFlavors":["` + pvFlavor + `"]}],` +
				`"flavors":[{"flavorId":"` + pvFlavor + `","name":"gen-2-4","cpu":2,"ram":4}],` +
				`"channels":[{"channel":"stable","availableVersions":["v1.31.4"]}]}`)
		case p == "/v3/clusters" && get:
			return write(`{"clusters":[]}`)
		case strings.HasSuffix(p, ":getKubeConfig"):
			kc := "apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster:\n    server: https://192.0.2.1:6443\ncontexts:\n- name: c\n  context:\n    cluster: c\n    user: u\ncurrent-context: c\nusers:\n- name: u\n  user:\n    token: fake\n"
			return write(`{"config":"` + base64.StdEncoding.EncodeToString([]byte(kc)) + `"}`)
		case strings.HasPrefix(p, "/v3/operations/") || !get:
			return write(jsonOp)
		}
	case "postgresql.api.cloud.ru", "kafka.api.cloud.ru", "redis.api.cloud.ru":
		switch {
		case strings.HasSuffix(p, "/databases") && get:
			return write(`{"databases":[{"name":"old"}]}`)
		case strings.HasSuffix(p, "/users") && get:
			return write(`{"users":[{"name":"old"}]}`)
		case strings.HasSuffix(p, "/backups") && get:
			return write(`{"backups":[]}`)
		case strings.HasPrefix(p, "/v1/operations/") || !get:
			return write(jsonOp)
		}
	case "vpc.api.cloud.ru", "ar.api.cloud.ru", "containers.api.cloud.ru", "nlb.api.cloud.ru":
		if strings.Contains(p, "/operations/") || !get {
			return write(jsonOp)
		}
	case "secretmanager.api.cloud.ru":
		if p == "/v2/secret" && r.Method == http.MethodPost {
			return write(`{"secret":{"id":"` + parityVM + `","path":"app/db"}}`)
		}
	case "certificatemanager.api.cloud.ru":
		switch {
		case p == "/v1/certificate" && r.Method == http.MethodPost:
			return write(`{"id":"` + parityVM + `","name":"byo"}`)
		case p == "/v1/certificate:issue_lenc_cert":
			return write(`{"id":"` + parityVM + `","done":false}`)
		}
	}
	return nil
}

// s3Parity is Object Storage with one bucket, bucket-web, holding k.txt.
func s3Parity(r *http.Request) *http.Response {
	w := httptest.NewRecorder()
	xml := func(body string) *http.Response {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+body)
		return w.Result()
	}
	const created = "2026-10-20T10:00:00.000Z"
	switch p := strings.Trim(r.URL.Path, "/"); {
	case r.Method == http.MethodGet && p == "":
		return xml(`<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>o</ID></Owner><Buckets>` +
			`<Bucket><Name>bucket-web</Name><CreationDate>` + created + `</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`)
	case r.Method == http.MethodGet && p == "bucket-web":
		return xml(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>bucket-web</Name><Prefix></Prefix><KeyCount>1</KeyCount>` +
			`<MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated><Contents><Key>k.txt</Key><LastModified>` + created + `</LastModified>` +
			`<ETag>"5d41402abc4b2a76b9719d911017c592"</ETag><Size>5</Size><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>`)
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && p == "bucket-web/k.txt":
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "5")
		w.Header().Set("ETag", `"5d41402abc4b2a76b9719d911017c592"`)
		w.Header().Set("Last-Modified", "Tue, 20 Oct 2026 10:00:00 GMT")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "hello")
		}
		return w.Result()
	case r.Method == http.MethodPut:
		w.Header().Set("ETag", `"5d41402abc4b2a76b9719d911017c592"`)
		return w.Result()
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
		return w.Result()
	}
	w.WriteHeader(404)
	return xml(`<Error><Code>NoSuchKey</Code><Message>no such path</Message></Error>`)
}

// jsonCloud is the compute fake of the previews with what the answers
// need: a free floating IP to attach, the security group web, and the VM
// gone after its deletion.
type jsonCloud struct {
	parityCloud
	fake *jsonFake
}

func (c jsonCloud) ListVMsInProject(ctx context.Context, project string) ([]*provision.VM, error) {
	if c.fake.gone() {
		return nil, nil
	}
	return c.parityCloud.ListVMsInProject(ctx, project)
}

func (c jsonCloud) ListSecurityGroups(ctx context.Context, project string) ([]*provision.SecurityGroup, error) {
	groups, err := c.parityCloud.ListSecurityGroups(ctx, project)
	return append(groups, &provision.SecurityGroup{ID: parityVM, Name: "web", AvailabilityZoneID: pvZone}), err
}

func (jsonCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: parityVM, IPAddress: "203.0.113.10", State: "available"}}, nil
}

// parityDeployer is the SSH of the VM: every command succeeds, the log is
// one line.
type parityDeployer struct{}

func (parityDeployer) Connect(context.Context, string, int, string, []byte, string) error { return nil }
func (parityDeployer) HostKeyFingerprint() string                                         { return "SHA256:fake" }
func (parityDeployer) Upload(context.Context, []deploy.FileEntry) error                   { return nil }
func (parityDeployer) Exec(context.Context, string) (string, error)                       { return "", nil }
func (parityDeployer) ExecStream(_ context.Context, _ string, stdout, _ io.Writer) error {
	_, err := io.WriteString(stdout, "web-1  | ready\n")
	return err
}
func (parityDeployer) Close() error { return nil }

// withJSONWorld is withParityWorld with the fixtures of the answers, the
// keys of Object Storage and the billing agreement, kubectl and helm that
// print a line; SSH, the health checks of deploy and the waits are
// withParityWorld's.
func withJSONWorld(t *testing.T) (parityWorld, *jsonFake) {
	t.Helper()
	w := withParityWorld(t)
	fake := &jsonFake{}
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		fake.record(r)
		return fake.answer(r), nil
	}
	t.Setenv("CLOUDRU_S3_ACCESS_KEY", "parity-access")
	t.Setenv("CLOUDRU_S3_SECRET_KEY", "parity-secret")
	t.Setenv("CLOUDRU_AGREEMENT_ID", parityVM)
	// kubectl and helm get a kubeconfig whose token evo mints from the key.
	t.Setenv("CLOUDRU_KEY_ID", "00000000000000000000000000000000")
	t.Setenv("CLOUDRU_SECRET", "00000000000000000000000000000000")
	putFakeKube(t, "echo ok")
	oldWait := deployplan.SSHPortWait
	deployplan.SSHPortWait = 0 // no TCP to the fake address
	t.Cleanup(func() { deployplan.SSHPortWait = oldWait })
	world := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := world(o)
		if err != nil {
			return nil, err
		}
		pc := e.Cloud.(parityCloud)
		if a, ok := pc.CloudAPI.(*provision.CloudRuAdapter); ok {
			a.Backoff = time.Millisecond // the retries of a failing cloud (TestParity_ErrorReasonEqualsTool)
		}
		e.Cloud = jsonCloud{parityCloud: pc, fake: fake}
		return e, nil
	}
	t.Cleanup(func() { toolEngine = world })
	return w, fake
}

// recordUnchanged is deploy's third state: the deployment of the directory
// runs the compose file as it is, and deploy has nothing to do.
func recordUnchanged(t *testing.T, w parityWorld) (map[string]any, func()) {
	t.Helper()
	args, done := recordDeployment(t, w)
	compose, err := os.ReadFile(filepath.Join(w.dir, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	path := state.DefaultPath(w.dir)
	f, err := state.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Deployments[0].ComposeHash = fmt.Sprintf("%x", sha256.Sum256(compose))
	if err := state.Write(path, f); err != nil {
		t.Fatal(err)
	}
	return args, done
}
