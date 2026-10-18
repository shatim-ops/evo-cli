// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/verify"
)

// --- Mocks ---

type mockTokenProvider struct {
	token auth.Token
	err   error
}

func (m *mockTokenProvider) Token(_ context.Context) (auth.Token, error) {
	return m.token, m.err
}

type mockCloudAPI struct {
	// Embed the interface so the mock satisfies CloudAPI even as new methods
	// are added; only the methods explicitly defined below are exercised (any
	// unimplemented one would panic if called, surfacing a missing stub).
	provision.CloudAPI

	mu sync.Mutex

	createVMFn              func(ctx context.Context, req provision.CreateVMRequest) (*provision.VM, error)
	getVMFn                 func(ctx context.Context, id string) (*provision.VM, error)
	deleteVMFn              func(ctx context.Context, id string) error
	listVMsInProjectFn      func(ctx context.Context, projectID string) ([]*provision.VM, error)
	listFlavorsFn           func(ctx context.Context) ([]*provision.Flavor, error)
	listAvailabilityZonesFn func(ctx context.Context) ([]*provision.AvailabilityZone, error)
	listImagesFn            func(ctx context.Context) ([]*provision.Image, error)
	// projectViewFn answers GET /images?project_id= (the catalogue as the
	// project sees it); nil: that call fails, so nothing is marked as
	// unavailable to the project.
	projectViewFn        func(ctx context.Context, f provision.ImageFilter) ([]*provision.Image, error)
	getImageFn           func(ctx context.Context, id string) (*provision.ImageDetail, error)
	getVMDetailFn        func(ctx context.Context, id string) (*provision.VMDetail, error)
	createFloatingIPFn   func(ctx context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error)
	getFloatingIPFn      func(ctx context.Context, id string) (*provision.FloatingIP, error)
	listFloatingIPsFn    func(ctx context.Context, projectID string) ([]*provision.FloatingIP, error)
	deleteFloatingIPFn   func(ctx context.Context, fipID, projectID string) error
	listDiskTypesFn      func(ctx context.Context, projectID string) ([]*provision.DiskType, error)
	listSubnetsFn        func(ctx context.Context, projectID string) ([]*provision.Subnet, error)
	listSecurityGroupsFn func(ctx context.Context, projectID string) ([]*provision.SecurityGroup, error)
	addedRules           []provision.SGRule
	addRuleFn            func(ctx context.Context, sgID string, rule provision.SGRule) (*provision.SGRule, error)
	deleteRuleFn         func(ctx context.Context, sgID, ruleID string) error
	deletedRules         []string // "sgID/ruleID"
	ruleSeq              int
	// priceFn answers price-calculation; nil: the call fails, so previews
	// show the rate-card estimate (as before the API was used).
	priceFn func(ctx context.Context, projectID string, req provision.PriceCalculationRequest) (*provision.PriceCalculation, error)

	token        string
	createCalled bool
	createReq    provision.CreateVMRequest
	getCalls     int
	deletedFIPs  []string
}

func (m *mockCloudAPI) SetToken(t string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = t
}

func (m *mockCloudAPI) CreateVM(ctx context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	m.mu.Lock()
	m.createCalled = true
	m.createReq = req
	m.mu.Unlock()
	if m.createVMFn != nil {
		return m.createVMFn(ctx, req)
	}
	return &provision.VM{
		ID:       "vm-123",
		Status:   "build",
		PublicIP: "127.0.0.1",
	}, nil
}

func (m *mockCloudAPI) GetVM(ctx context.Context, id string) (*provision.VM, error) {
	m.mu.Lock()
	m.getCalls++
	m.mu.Unlock()
	if m.getVMFn != nil {
		return m.getVMFn(ctx, id)
	}
	return &provision.VM{
		ID:       id,
		Status:   "active",
		PublicIP: "127.0.0.1",
	}, nil
}

func (m *mockCloudAPI) DeleteVM(ctx context.Context, id string) error {
	if m.deleteVMFn != nil {
		return m.deleteVMFn(ctx, id)
	}
	return nil
}

func (m *mockCloudAPI) ListFlavors(ctx context.Context) ([]*provision.Flavor, error) {
	if m.listFlavorsFn != nil {
		return m.listFlavorsFn(ctx)
	}
	return []*provision.Flavor{
		{ID: "f-small", Name: "small", VCPUs: 2, RAMGB: 2},
		{ID: "f-medium", Name: "medium", VCPUs: 4, RAMGB: 4},
	}, nil
}

func (m *mockCloudAPI) ListAvailabilityZones(ctx context.Context) ([]*provision.AvailabilityZone, error) {
	if m.listAvailabilityZonesFn != nil {
		return m.listAvailabilityZonesFn(ctx)
	}
	return []*provision.AvailabilityZone{
		{ID: "az-1", Name: "ru-moscow-1"},
		{ID: "az-2", Name: "ru-moscow-2"},
	}, nil
}

func (m *mockCloudAPI) ListImages(ctx context.Context, filter ...provision.ImageFilter) ([]*provision.Image, error) {
	if len(filter) > 0 && filter[0].ProjectID != "" {
		if m.projectViewFn != nil {
			return m.projectViewFn(ctx, filter[0])
		}
		return nil, fmt.Errorf("mock: no project view of the catalogue")
	}
	if m.listImagesFn != nil {
		return m.listImagesFn(ctx)
	}
	return []*provision.Image{
		{ID: "img-1", Name: "Ubuntu 22.04", OS: "linux", Status: "active"},
	}, nil
}

func (m *mockCloudAPI) ListImagesInProject(ctx context.Context, projectID string) ([]*provision.Image, error) {
	return m.ListImages(ctx)
}

func (m *mockCloudAPI) GetImage(ctx context.Context, id string) (*provision.ImageDetail, error) {
	if m.getImageFn != nil {
		return m.getImageFn(ctx, id)
	}
	return &provision.ImageDetail{
		ID:   id,
		Name: "Ubuntu 22.04",
		OS:   "linux",
		MetadataFields: []provision.ImageMetadataField{
			{ID: "mf-1", Name: "ssh_key", MandatoryGroup: "ssh"},
			{ID: "mf-2", Name: "hostname"},
			{ID: "mf-3", Name: "name", MandatoryGroup: "name", DefaultValue: "user1"},
		},
	}, nil
}

func (m *mockCloudAPI) CreateImage(ctx context.Context, req provision.CreateImageReq) (map[string]any, error) {
	return map[string]any{"id": "img-new", "name": req.Name, "disk_id": req.DiskID}, nil
}

func (m *mockCloudAPI) DeleteImage(ctx context.Context, imageID, projectID string) error {
	return nil
}

func (m *mockCloudAPI) GetVMDetail(ctx context.Context, id string) (*provision.VMDetail, error) {
	if m.getVMDetailFn != nil {
		return m.getVMDetailFn(ctx, id)
	}
	return &provision.VMDetail{
		ID:    id,
		Name:  "test-vm",
		State: "running",
		Interfaces: []provision.VMInterface{
			{ID: "iface-1", IPAddress: "10.0.0.8", Primary: true, State: "created"},
		},
	}, nil
}

func (m *mockCloudAPI) CreateFloatingIP(ctx context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	if m.createFloatingIPFn != nil {
		return m.createFloatingIPFn(ctx, req)
	}
	return &provision.FloatingIP{
		ID:        "fip-1",
		Name:      req.Name,
		IPAddress: "127.0.0.1",
		State:     "in_use",
	}, nil
}

func (m *mockCloudAPI) GetFloatingIP(ctx context.Context, id string) (*provision.FloatingIP, error) {
	if m.getFloatingIPFn != nil {
		return m.getFloatingIPFn(ctx, id)
	}
	return &provision.FloatingIP{
		ID:        id,
		IPAddress: "127.0.0.1",
		State:     "in_use",
	}, nil
}

func (m *mockCloudAPI) ListFloatingIPs(ctx context.Context, projectID string) ([]*provision.FloatingIP, error) {
	if m.listFloatingIPsFn != nil {
		return m.listFloatingIPsFn(ctx, projectID)
	}
	return nil, nil
}

func (m *mockCloudAPI) ListVMsInProject(ctx context.Context, projectID string) ([]*provision.VM, error) {
	if m.listVMsInProjectFn != nil {
		return m.listVMsInProjectFn(ctx, projectID)
	}
	return nil, nil
}

func (m *mockCloudAPI) DeleteFloatingIP(ctx context.Context, fipID, projectID string) error {
	m.mu.Lock()
	m.deletedFIPs = append(m.deletedFIPs, fipID)
	m.mu.Unlock()
	if m.deleteFloatingIPFn != nil {
		return m.deleteFloatingIPFn(ctx, fipID, projectID)
	}
	return nil
}

func (m *mockCloudAPI) ListDiskTypes(ctx context.Context, projectID string) ([]*provision.DiskType, error) {
	if m.listDiskTypesFn != nil {
		return m.listDiskTypesFn(ctx, projectID)
	}
	return []*provision.DiskType{
		{ID: "dt-ssd", Name: "SSD"},
		{ID: "dt-hdd", Name: "HDD"},
	}, nil
}

func (m *mockCloudAPI) PriceCalculation(ctx context.Context, projectID string, req provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	if m.priceFn != nil {
		return m.priceFn(ctx, projectID, req)
	}
	return nil, errNoPriceAPI
}

type mockDeployer struct {
	mu sync.Mutex

	connectErr      error
	uploadErr       error
	execResults     map[string]string
	execErr         error
	streamErrs      map[string][]error                          // per-command errors for ExecStream, consumed in order
	streamHook      func(ctx context.Context, cmd string) error // runs first; a non-nil error is returned
	streamOut       func(cmd string) string                     // written to stdout before streamHook/streamErrs
	hostFingerprint string
	connectCalled   bool
	connectCalls    int
	uploadCalled    bool
	execCalls       []string
	closeCalled     bool
	uploadedFiles   []deploy.FileEntry
}

func newMockDeployer() *mockDeployer {
	return &mockDeployer{
		execResults: map[string]string{
			dockerReadyCmd: "Docker Compose version v2.24.0",
		},
	}
}

func (m *mockDeployer) Connect(_ context.Context, host string, port int, user string, keyPEM []byte, knownHostKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectCalled = true
	m.connectCalls++
	return m.connectErr
}

func (m *mockDeployer) HostKeyFingerprint() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hostFingerprint == "" {
		return "SHA256:testfingerprint"
	}
	return m.hostFingerprint
}

func (m *mockDeployer) Upload(_ context.Context, files []deploy.FileEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploadCalled = true
	m.uploadedFiles = append(m.uploadedFiles, files...)
	return m.uploadErr
}

func (m *mockDeployer) Exec(_ context.Context, cmd string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execCalls = append(m.execCalls, cmd)
	if m.execErr != nil {
		return "", m.execErr
	}
	if result, ok := m.execResults[cmd]; ok {
		return result, nil
	}
	return "", nil
}

// ExecStream records the call like Exec and writes the scripted result.
func (m *mockDeployer) ExecStream(ctx context.Context, cmd string, stdout, _ io.Writer) error {
	if m.streamOut != nil {
		_, _ = io.WriteString(stdout, m.streamOut(cmd))
	}
	if m.streamHook != nil {
		if err := m.streamHook(ctx, cmd); err != nil {
			m.mu.Lock()
			m.execCalls = append(m.execCalls, cmd)
			m.mu.Unlock()
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execCalls = append(m.execCalls, cmd)
	if errs := m.streamErrs[cmd]; len(errs) > 0 {
		m.streamErrs[cmd] = errs[1:]
		return errs[0]
	}
	if m.execErr != nil {
		return m.execErr
	}
	_, _ = io.WriteString(stdout, m.execResults[cmd])
	return nil
}

func (m *mockDeployer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return nil
}

// --- Helpers ---

var testComposeContent = `services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
  db:
    image: postgres:15
    ports:
      - "5432:5432"
`

func writeComposeFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte(testComposeContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func composeHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(testComposeContent)))
}

// fastTimings replaces every production wait with a millisecond so that
// deploy tests exercise the poll/retry loops without sleeping for real.
var fastTimings = Timings{
	VMPollInterval:    time.Millisecond,
	VMPollTimeout:     5 * time.Second,
	IfacePollInterval: time.Millisecond,
	FIPRetryInterval:  time.Millisecond,
	FIPLookupInterval: time.Millisecond,
	FIPPollInterval:   time.Millisecond,
	SSHRetryInterval:  time.Millisecond,
	SettleDelay:       time.Millisecond,
	VerifyTimeout:     time.Second,

	DockerPollInterval:  time.Millisecond,
	DockerWaitTimeout:   5 * time.Second,
	DestroyPollInterval: time.Millisecond,
	DestroyPollTimeout:  5 * time.Second,

	DeletableWaitTimeout:  50 * time.Millisecond,
	DeletablePollInterval: time.Millisecond,
}

// stubProbe records health-check calls instead of opening connections, so
// tests never dial the ports from the compose file.
type stubProbe struct {
	mu    sync.Mutex
	ip    string
	ports []int
	calls int
}

func (p *stubProbe) Check(_ context.Context, ip string, ports []int, _ time.Duration) []verify.ProbeResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.ip = ip
	p.ports = append([]int(nil), ports...)
	res := make([]verify.ProbeResult, len(ports))
	for i, port := range ports {
		res[i] = verify.ProbeResult{Port: port, StatusCode: 200, Healthy: true}
	}
	return res
}

func newTestEngine(t *testing.T, cloud *mockCloudAPI, deployer *mockDeployer) (*Engine, string) {
	t.Helper()
	tmpDir := t.TempDir()

	var buf bytes.Buffer
	printer := output.NewWithWriter(&buf, output.ModePlain, false)
	probe := &stubProbe{}

	return &Engine{
		Config: config.Config{
			ProjectID: "00000000-0000-0000-0000-000000000001",
			ImageID:   "00000000-0000-0000-0000-000000000002",
			DiskSize:  10,
		},
		Auth:      &mockTokenProvider{token: auth.Token{Value: "test-token", ExpiresAt: time.Now().Add(time.Hour)}},
		Cloud:     cloud,
		Deployer:  deployer,
		Printer:   printer,
		SSHKeyPEM: []byte("fake-key-pem"),
		SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
		StateDir:  tmpDir,
		Timings:   fastTimings,
		Probe:     probe.Check,
	}, tmpDir
}

// --- Tests ---

func TestDeploy_NewDeploy(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, tmpDir := newTestEngine(t, cloud, dep)

	composePath := writeComposeFile(t, tmpDir)

	opts := DeployOptions{
		ComposeFile:    composePath,
		Flavor:         "small",
		Region:         "az-1",
		NonInteractive: true,
		Yes:            true,
	}

	err := eng.Deploy(context.Background(), opts)
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	// Verify stages were called.
	if !cloud.createCalled {
		t.Error("expected CreateVM to be called")
	}
	cloud.mu.Lock()
	gotToken := cloud.token
	cloud.mu.Unlock()
	if gotToken != "test-token" {
		t.Errorf("expected token 'test-token', got %q", gotToken)
	}

	dep.mu.Lock()
	connectCalled := dep.connectCalled
	uploadCalled := dep.uploadCalled
	execCalls := append([]string{}, dep.execCalls...)
	closeCalled := dep.closeCalled
	dep.mu.Unlock()

	if !connectCalled {
		t.Error("expected SSH Connect to be called")
	}
	if !uploadCalled {
		t.Error("expected Upload to be called")
	}
	if !closeCalled {
		t.Error("expected Close to be called")
	}

	// Verify docker compose version check and compose up were called.
	foundVersion := false
	foundUp := false
	for _, cmd := range execCalls {
		if cmd == dockerReadyCmd {
			foundVersion = true
		}
		if cmd == "cd /opt/app && docker compose -f docker-compose.yml up -d" {
			foundUp = true
		}
	}
	if !foundVersion {
		t.Errorf("expected %q exec call", dockerReadyCmd)
	}
	if !foundUp {
		t.Errorf("expected 'docker compose up -d' exec call, got: %v", execCalls)
	}

	// Verify state was written.
	statePath := state.DefaultPath(tmpDir)
	sf, err := state.Read(statePath)
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	if len(sf.Deployments) == 0 {
		t.Fatal("expected at least one deployment in state")
	}
	if sf.Deployments[0].Status != "running" {
		t.Errorf("expected status 'running', got %q", sf.Deployments[0].Status)
	}
	if sf.Deployments[0].VMID != "vm-123" {
		t.Errorf("expected VMID 'vm-123', got %q", sf.Deployments[0].VMID)
	}
}

func TestDeploy_UpdateDeploy(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, tmpDir := newTestEngine(t, cloud, dep)

	composePath := writeComposeFile(t, tmpDir)

	// Write existing state with a different hash.
	statePath := state.DefaultPath(tmpDir)
	existingState := &state.File{
		Version: 1,
		Deployments: []state.DeploymentRecord{
			{
				ID:          "vm-existing",
				VMID:        "vm-existing",
				VMIP:        "127.0.0.1",
				VMFlavor:    "small",
				Region:      "az-1",
				Status:      "running",
				SSHUser:     "user1",
				ComposeHash: "oldhash-different-from-current",
				CreatedAt:   time.Now().Add(-time.Hour),
				UpdatedAt:   time.Now().Add(-time.Hour),
			},
		},
	}
	if err := state.Write(statePath, existingState); err != nil {
		t.Fatal(err)
	}

	opts := DeployOptions{
		ComposeFile:    composePath,
		Flavor:         "small",
		Region:         "az-1",
		NonInteractive: true,
		Yes:            true,
	}

	err := eng.Deploy(context.Background(), opts)
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	// Provision should NOT have been called — reusing existing VM.
	if cloud.createCalled {
		t.Error("expected CreateVM NOT to be called on update deploy")
	}

	// SSH and deploy should have been called.
	dep.mu.Lock()
	uploadCalled := dep.uploadCalled
	dep.mu.Unlock()
	if !uploadCalled {
		t.Error("expected Upload to be called on update deploy")
	}

	// State should now reflect the new hash.
	sf, err := state.Read(statePath)
	if err != nil {
		t.Fatal(err)
	}
	active := sf.ActiveDeployment()
	if active == nil {
		t.Fatal("expected active deployment")
	}
	if active.ComposeHash == "oldhash-different-from-current" {
		t.Error("expected compose hash to be updated")
	}
	if active.Status != "running" {
		t.Errorf("expected status 'running', got %q", active.Status)
	}
}

func TestDeploy_NoChanges(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, tmpDir := newTestEngine(t, cloud, dep)

	composePath := writeComposeFile(t, tmpDir)

	// Pre-write state with the same hash as the compose file.
	statePath := state.DefaultPath(tmpDir)
	existingState := &state.File{
		Version: 1,
		Deployments: []state.DeploymentRecord{
			{
				ID:          "vm-existing",
				VMID:        "vm-existing",
				VMIP:        "127.0.0.1",
				VMFlavor:    "small",
				Region:      "az-1",
				Status:      "running",
				SSHUser:     "user1",
				ComposeHash: composeHash(),
				CreatedAt:   time.Now().Add(-time.Hour),
				UpdatedAt:   time.Now().Add(-time.Hour),
			},
		},
	}
	if err := state.Write(statePath, existingState); err != nil {
		t.Fatal(err)
	}

	opts := DeployOptions{
		ComposeFile:    composePath,
		Flavor:         "small",
		Region:         "az-1",
		NonInteractive: true,
		Yes:            true,
	}

	err := eng.Deploy(context.Background(), opts)
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	// Nothing should have been called.
	if cloud.createCalled {
		t.Error("expected CreateVM NOT to be called when no changes")
	}
	dep.mu.Lock()
	connectCalled := dep.connectCalled
	dep.mu.Unlock()
	if connectCalled {
		t.Error("expected SSH Connect NOT to be called when no changes")
	}
}

func TestDeploy_ForceRedeploy(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, tmpDir := newTestEngine(t, cloud, dep)

	composePath := writeComposeFile(t, tmpDir)

	// Pre-write state with the same hash.
	statePath := state.DefaultPath(tmpDir)
	existingState := &state.File{
		Version: 1,
		Deployments: []state.DeploymentRecord{
			{
				ID:          "vm-existing",
				VMID:        "vm-existing",
				VMIP:        "127.0.0.1",
				VMFlavor:    "small",
				Region:      "az-1",
				Status:      "running",
				SSHUser:     "user1",
				ComposeHash: composeHash(),
				CreatedAt:   time.Now().Add(-time.Hour),
				UpdatedAt:   time.Now().Add(-time.Hour),
			},
		},
	}
	if err := state.Write(statePath, existingState); err != nil {
		t.Fatal(err)
	}

	opts := DeployOptions{
		ComposeFile:    composePath,
		Flavor:         "small",
		Region:         "az-1",
		Force:          true,
		NonInteractive: true,
		Yes:            true,
	}

	err := eng.Deploy(context.Background(), opts)
	if err != nil {
		t.Fatalf("Force deploy returned error: %v", err)
	}

	// Should NOT create a new VM (existing is reused), but should deploy.
	if cloud.createCalled {
		t.Error("expected CreateVM NOT to be called on force redeploy (VM exists)")
	}
	dep.mu.Lock()
	uploadCalled := dep.uploadCalled
	connectCalled := dep.connectCalled
	dep.mu.Unlock()
	if !connectCalled {
		t.Error("expected SSH Connect to be called on force redeploy")
	}
	if !uploadCalled {
		t.Error("expected Upload to be called on force redeploy")
	}
}

func (m *mockCloudAPI) ListSubnets(ctx context.Context, projectID string) ([]*provision.Subnet, error) {
	if m.listSubnetsFn != nil {
		return m.listSubnetsFn(ctx, projectID)
	}
	return []*provision.Subnet{{ID: "subnet-1", Name: "Default_ru-moscow-1"}}, nil
}

func (m *mockCloudAPI) ListSecurityGroups(ctx context.Context, projectID string) ([]*provision.SecurityGroup, error) {
	if m.listSecurityGroupsFn != nil {
		return m.listSecurityGroupsFn(ctx, projectID)
	}
	return []*provision.SecurityGroup{{ID: "sg-1", Name: "SSH-access_ru-moscow-1"}}, nil
}

// AddSecurityGroupRule records the rule and, by default, answers as the
// documented API does: the rule with a fresh ID.
func (m *mockCloudAPI) AddSecurityGroupRule(ctx context.Context, sgID, projectID string, rule provision.SGRule) (*provision.SGRule, error) {
	if m.addRuleFn != nil {
		created, err := m.addRuleFn(ctx, sgID, rule)
		if err == nil {
			m.mu.Lock()
			m.addedRules = append(m.addedRules, rule)
			m.mu.Unlock()
		}
		return created, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ruleSeq++
	rule.ID = fmt.Sprintf("rule-%d", m.ruleSeq)
	m.addedRules = append(m.addedRules, rule)
	created := rule
	return &created, nil
}

// ListSecurityGroupRules answers with the rules the group list fixture
// carries for sgID.
func (m *mockCloudAPI) ListSecurityGroupRules(ctx context.Context, sgID string) ([]*provision.SGRule, error) {
	sgs, err := m.ListSecurityGroups(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []*provision.SGRule
	for _, g := range sgs {
		if g.ID == sgID {
			for i := range g.Rules {
				out = append(out, &g.Rules[i])
			}
		}
	}
	return out, nil
}

func (m *mockCloudAPI) DeleteSecurityGroupRule(ctx context.Context, sgID, ruleID string) error {
	if m.deleteRuleFn != nil {
		if err := m.deleteRuleFn(ctx, sgID, ruleID); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedRules = append(m.deletedRules, sgID+"/"+ruleID)
	return nil
}
