package engine

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The zero Timings must keep the production waits unchanged.
func TestTimings_ZeroMeansProductionDefaults(t *testing.T) {
	got := Timings{}.withDefaults()
	want := Timings{
		VMPollInterval:     5 * time.Second,
		VMPollTimeout:      10 * time.Minute,
		VMProgressInterval: 30 * time.Second,
		IfacePollInterval:  5 * time.Second,
		FIPRetryInterval:   10 * time.Second,
		FIPLookupInterval:  2 * time.Second,
		FIPPollInterval:    5 * time.Second,
		SSHRetryInterval:   5 * time.Second,
		SettleDelay:        2 * time.Second,
		VerifyTimeout:      10 * time.Second,

		DockerPollInterval:  10 * time.Second,
		DockerWaitTimeout:   10 * time.Minute,
		DestroyPollInterval: 3 * time.Second,
		DestroyPollTimeout:  2 * time.Minute,

		DeletableWaitTimeout:  10 * time.Minute,
		DeletablePollInterval: 10 * time.Second,

		StatusRefreshTimeout: 8 * time.Second,
	}
	if got != want {
		t.Errorf("defaults changed:\n got  %+v\n want %+v", got, want)
	}

	// A partially filled Timings keeps the set field and defaults the rest.
	part := Timings{SSHRetryInterval: time.Millisecond}.withDefaults()
	if part.SSHRetryInterval != time.Millisecond || part.VMPollInterval != vmPollInterval {
		t.Errorf("partial override broken: %+v", part)
	}
}

// Deploy hands the VM's public IP and the compose ports to the health probe.
func TestDeploy_ProbeGetsVMIPAndPorts(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, tmpDir := newTestEngine(t, cloud, dep)
	probe := &stubProbe{}
	eng.Probe = probe.Check

	err := eng.Deploy(context.Background(), DeployOptions{
		ComposeFile:    writeComposeFile(t, tmpDir),
		Flavor:         "small",
		NonInteractive: true,
		Yes:            true,
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.calls != 1 {
		t.Fatalf("probe calls = %d, want 1", probe.calls)
	}
	if probe.ip != "127.0.0.1" {
		t.Errorf("probe ip = %q, want floating IP 127.0.0.1", probe.ip)
	}
	if fmt.Sprint(probe.ports) != "[5432 8080]" {
		t.Errorf("probe ports = %v, want [5432 8080]", probe.ports)
	}
}

// With Probe unset Deploy uses the real verify.Check; an httptest server on
// localhost stands in for the deployed service.
func TestDeploy_DefaultProbeHitsService(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, tmpDir := newTestEngine(t, cloud, dep)
	eng.Probe = nil
	var out bytes.Buffer
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)

	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	compose := fmt.Sprintf("services:\n  web:\n    image: nginx:latest\n    ports:\n      - \"%d:80\"\n", port)
	if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}

	err := eng.Deploy(context.Background(), DeployOptions{
		ComposeFile:    composePath,
		Flavor:         "small",
		NonInteractive: true,
		Yes:            true,
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("service hits = %d, want 1", hits.Load())
	}
	if want := fmt.Sprintf(":%d -> 200", port); !strings.Contains(out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, out.String())
	}
}

// If the primary interface never reports "created", assignFloatingIP falls
// back to the primary interface after ifaceFallbackAttempt polls.
func TestAssignFloatingIP_InterfaceStateFallback(t *testing.T) {
	var polls atomic.Int32
	cloud := &mockCloudAPI{
		getVMDetailFn: func(_ context.Context, id string) (*provision.VMDetail, error) {
			polls.Add(1)
			return &provision.VMDetail{
				ID:         id,
				Interfaces: []provision.VMInterface{{ID: "iface-1", Primary: true, State: "creating"}},
			}, nil
		},
	}
	var gotIface string
	cloud.createFloatingIPFn = func(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		gotIface = req.InterfaceID
		return &provision.FloatingIP{ID: "fip-1", IPAddress: "127.0.0.1", State: "in_use"}, nil
	}

	ip, _, err := assignFloatingIP(context.Background(), cloud, fastTimings, nil, "vm-1", "proj", "az")
	if err != nil {
		t.Fatalf("assignFloatingIP: %v", err)
	}
	if ip != "127.0.0.1" || gotIface != "iface-1" {
		t.Errorf("ip=%q iface=%q", ip, gotIface)
	}
	if n := polls.Load(); n != ifaceFallbackAttempt+1 {
		t.Errorf("GetVMDetail polls = %d, want %d", n, ifaceFallbackAttempt+1)
	}
}

// CLOUDRU_TIMEOUT=0 or a negative value means the default wait, not "give
// up at once".
func TestTimings_NonPositiveVMPollTimeoutIsDefault(t *testing.T) {
	for _, in := range []time.Duration{0, -5 * time.Second} {
		if got := (Timings{VMPollTimeout: in}).withDefaults().VMPollTimeout; got != vmPollTimeout {
			t.Errorf("VMPollTimeout %v → %v, want %v", in, got, vmPollTimeout)
		}
	}
}
