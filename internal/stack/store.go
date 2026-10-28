// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package stack is a crank-driven, disk-persisted orchestrator for multi-resource
// Cloud.ru "stacks" (PG + VM + NLB + secrets + S3 in one declarative manifest).
//
// WHY: an agent that creates 8 services by calling ~20 tools in ONE reasoning
// turn accumulates the whole tool-call history in its context and blows the
// per-turn trajectory/context limit ("couldn't generate a response") — worse on
// small-context LLMs. This module moves the execution loop OUT of the agent: the
// agent submits a manifest (stack_apply) and then polls a COMPACT status
// (stack_status). The agent's context stays O(1) regardless of stack size.
//
// The MCP server is spawned per stdio call (no daemon), so the orchestrator is a
// RESUMABLE STATE MACHINE persisted to disk: each stack_status call is one "crank"
// that loads state, advances as far as it can (poll in-flight async ops, start
// ready steps), saves, and returns. State lives on disk, so it survives agent
// context loss (/new, compaction, a different LLM) — just re-poll the job_id.
package stack

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// --- Manifest (desired state) ---

type Manifest struct {
	Name     string        `json:"name"`
	VPC      *VPCSpec      `json:"vpc,omitempty"`
	Postgres *PGSpec       `json:"postgres,omitempty"`
	Kafka    *KafkaSpec    `json:"kafka,omitempty"`
	Redis    *RedisSpec    `json:"redis,omitempty"`
	K8s      *K8sSpec      `json:"k8s,omitempty"`
	Registry *RegistrySpec `json:"registry,omitempty"`
	SNAT     *SNATSpec     `json:"snat,omitempty"`
	VM       *VMSpec       `json:"vm,omitempty"`
	NLB      *NLBSpec      `json:"nlb,omitempty"`
	DNS      *DNSSpec      `json:"dns,omitempty"`
	Secrets  []SecretSpec  `json:"secrets,omitempty"`
	S3       []S3Spec      `json:"s3,omitempty"`
}

// VPCSpec creates a Virtual Private Cloud (free). Async.
type VPCSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// K8sSpec creates a Managed Kubernetes cluster (billable, about 10 min). Async;
// readiness = k8s_get status RUNNING. cluster_sa_id/subnet auto-resolve.
type K8sSpec struct {
	Name            string `json:"name"`
	Version         string `json:"version,omitempty"`
	Flavor          string `json:"flavor,omitempty"` // control-plane flavor
	CPU             int    `json:"cpu,omitempty"`
	RAM             int    `json:"ram_gb,omitempty"`
	MasterCount     int    `json:"master_count,omitempty"` // 1|3|5
	KubeAPIInternet bool   `json:"kube_api_internet,omitempty"`
}

// KafkaSpec creates a Managed Kafka cluster (billable, async; ready = READY).
type KafkaSpec struct {
	Name       string `json:"name"`
	VersionID  string `json:"version_id,omitempty"`
	SpecID     string `json:"specification_id,omitempty"`
	CPU        int    `json:"cpu,omitempty"`
	RAM        int    `json:"ram,omitempty"`
	StorageGiB int    `json:"storage_gib,omitempty"`
	Brokers    int    `json:"brokers,omitempty"`
	UIEnabled  bool   `json:"ui_enabled,omitempty"`
}

// RedisSpec creates a Managed Redis cluster (billable, async; ready = READY).
type RedisSpec struct {
	Name      string `json:"name"`
	VersionID string `json:"version_id,omitempty"`
	SpecID    string `json:"specification_id,omitempty"`
	CPU       int    `json:"cpu,omitempty"`
	RAM       int    `json:"ram,omitempty"`
	StorageGB int    `json:"storage_gb,omitempty"`
	Shards    int    `json:"shards,omitempty"`
	Replicas  int    `json:"replicas,omitempty"`
	Type      string `json:"type,omitempty"` // STANDALONE | MASTER_REPLICA
}

// RegistrySpec creates an Artifact Registry (billable). Async.
type RegistrySpec struct {
	Name   string `json:"name"`
	Type   string `json:"registry_type,omitempty"` // DOCKER default
	Public bool   `json:"public,omitempty"`
	Tariff string `json:"tariff,omitempty"`
}

// SNATSpec creates a SNAT gateway (billable) so private-subnet nodes get
// outbound internet. Synchronous.
type SNATSpec struct {
	Name string `json:"name"`
	Zone string `json:"zone,omitempty"` // default = configured region
}

// DNSSpec points a domain record at the stack's load balancer (or an explicit
// value). If Target=="lb" (default), the record value is the LB's external IP,
// resolved at run time — giving partners a URL instead of a raw IP.
type DNSSpec struct {
	ZoneID     string `json:"zone_id"`               // public zone id (from dns_zones)
	Name       string `json:"name"`                  // record name / subdomain
	RecordType string `json:"record_type,omitempty"` // default "a"
	Target     string `json:"target,omitempty"`      // "lb" (default) → LB external IP, else literal value
	TTL        int    `json:"ttl,omitempty"`
}

type PGSpec struct {
	Name          string `json:"name"`
	SpecID        string `json:"specification_id,omitempty"`
	CPU           int    `json:"cpu,omitempty"`
	RAM           int    `json:"ram,omitempty"`
	StorageGB     int    `json:"storage_gb,omitempty"`
	NoBackup      bool   `json:"no_backup,omitempty"`
	InitialDBName string `json:"initial_database,omitempty"`
}

type VMSpec struct {
	Name    string `json:"name"`
	Private bool   `json:"private,omitempty"` // no public IP → regular interface (NLB target)
	Flavor  string `json:"flavor,omitempty"`
	// Image and Zone: a name or UUID each; stack_plan resolves them to
	// UUIDs (default: the server's CLOUDRU_IMAGE_ID and CLOUDRU_REGION).
	// Without them every stack with a VM failed on its vm step, after the
	// earlier steps were created (release audit, L-36).
	Image string `json:"image,omitempty"`
	Zone  string `json:"zone,omitempty"`
}

type NLBSpec struct {
	Name       string `json:"name"`
	Port       int    `json:"port"`
	TargetPort int    `json:"target_port,omitempty"`
	External   bool   `json:"external,omitempty"`
	// Target is a logical ref to a step in this manifest, e.g. "vm".
	Target string `json:"target,omitempty"`
}

type SecretSpec struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

type S3Spec struct {
	Bucket  string `json:"bucket"`
	Key     string `json:"key"`
	Content string `json:"content"`
}

// --- Job (runtime state, persisted) ---

// Step is one unit of work. Args may contain "$stepkey" string placeholders that
// the runner substitutes from Outputs at execution time (e.g. tg's vm_id="$vm").
type Step struct {
	Key       string         `json:"key"`  // unique: postgres, vm, tg, lb, secret:0, s3:0
	Tool      string         `json:"tool"` // handler name to invoke
	Args      map[string]any `json:"args"`
	DependsOn []string       `json:"depends_on,omitempty"`
	// async handling
	PollTool  string `json:"poll_tool,omitempty"`  // operation-poll tool for ReadyKind "op" (lb_operation/vpc_operation/…)
	ReadyKind string `json:"ready_kind,omitempty"` // "" (sync) | "op" (poll operation) | "status" (poll a get tool for a ready status)
	// ReadyKind "status": poll StatusTool({StatusArg: id}) until StatusField contains ReadyValue
	StatusTool  string `json:"status_tool,omitempty"`  // pg_get / k8s_get
	StatusArg   string `json:"status_arg,omitempty"`   // id arg name: cluster_id
	StatusField string `json:"status_field,omitempty"` // "status"
	ReadyValue  string `json:"ready_value,omitempty"`  // "READY" / "RUNNING" (substring match)
	// runtime
	Status     string `json:"status"` // pending|running|waiting|done|error
	OpID       string `json:"op_id,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
	Attempts   int    `json:"attempts,omitempty"` // create retries used (transient-error tolerance)
	Detail     string `json:"detail,omitempty"`
	// Reason is the reason the step's tool answered its last failed call
	// (a code of internal/errors): stack_status names it for a step in
	// error (error_reasons, G140-5).
	Reason string `json:"reason,omitempty"`
	// Interrupted marks a create cut off by a cancelled call: it may have
	// reached the cloud, so the next crank looks for the resource before
	// creating it again.
	Interrupted bool `json:"interrupted,omitempty"`
	// StartedAt is when the step first called its create tool. A VM found by
	// name counts as the step's own only if it was created after it, so a
	// user's VM with the same name is never adopted (and never destroyed).
	StartedAt time.Time `json:"started_at,omitempty"`
}

type Job struct {
	ID       string   `json:"id"`
	Manifest Manifest `json:"manifest"`
	Status   string   `json:"status"` // running|done|error|destroying|destroyed
	// ProjectID is the Cloud.ru project the stack was planned in: its step
	// tools run in the server's project, so apply, status and destroy refuse
	// a server on another one. Empty in jobs from older versions.
	ProjectID string            `json:"project_id,omitempty"`
	Steps     []*Step           `json:"steps"`
	Outputs   map[string]string `json:"outputs"` // stepkey -> resource id; plus extras like lb_external_ip
	Error     string            `json:"error,omitempty"`
	// Deleting are the steps whose deletion the last stack_destroy saw
	// accepted but not finished (Postgres, NLB, …): the job is not done
	// with until a destroy finds them gone.
	Deleting []string `json:"deleting,omitempty"`
}

// --- Persistence ---

// jobsDir is ~/.evo/stack-jobs. Without a home directory it is an error, not
// a path relative to the working directory: job files hold the manifest's
// secret values, and the working directory may be a repository.
func jobsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New(i18n.T("no home directory (HOME is not set) for the stack jobs (~/.evo/stack-jobs)",
			"нет домашнего каталога (HOME не задан) для заданий стека (~/.evo/stack-jobs)"))
	}
	return filepath.Join(config.DataDir(home), "stack-jobs"), nil
}

func newID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "stk-" + hex.EncodeToString(b)
}

// OrphanRef summarizes a prior stack job of the same name that still holds
// created resources (used to warn about duplicate-billing risk at plan time).
type OrphanRef struct {
	JobID     string   `json:"job_id"`
	Status    string   `json:"status"`
	Resources []string `json:"resources"`
}

// FindResourcefulJobs scans persisted jobs for OTHER jobs of the same stack name
// that still hold created resources (status != destroyed). Re-planning while one
// exists risks duplicate billable resources (a failed job leaves e.g. a Postgres
// cluster; a fresh apply creates a second) — the caller surfaces these so the
// the user can stack_destroy the old job first.
func FindResourcefulJobs(name, excludeID string) []OrphanRef {
	dir, err := jobsDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []OrphanRef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if id == excludeID {
			continue
		}
		j, err := LoadJob(id)
		if err != nil || j.Manifest.Name != name || j.Status == "destroyed" {
			continue
		}
		var res []string
		for _, st := range j.Steps {
			if st.ResourceID != "" {
				res = append(res, st.Key+"="+st.ResourceID)
			}
		}
		if len(res) > 0 {
			out = append(out, OrphanRef{JobID: id, Status: j.Status, Resources: res})
		}
	}
	return out
}

// MayHaveCreated reports whether the step may have made its resource: it
// started, was interrupted mid-create, or used an attempt.
func (st *Step) MayHaveCreated() bool {
	if st.Interrupted || st.Attempts > 0 || st.OpID != "" {
		return true
	}
	switch st.Status {
	case "running", "waiting", "done":
		return true
	}
	return false
}

// SecretHolders are the jobs, other than excludeID, not destroyed, whose
// secret step for path may have made it: a second stack with the path would
// take that secret over, and its destroy would delete it (Grok live run on
// 1.3.0, G130-24; D41).
func SecretHolders(path, excludeID string) []string {
	dir, err := jobsDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if id == excludeID {
			continue
		}
		j, err := LoadJob(id)
		if err != nil || j.Status == "destroyed" {
			continue
		}
		for i, sc := range j.Manifest.Secrets {
			if sc.Path != path {
				continue
			}
			for _, st := range j.Steps {
				if st.Key == fmt.Sprintf("secret:%d", i) && st.MayHaveCreated() {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

// Save persists a job to disk: the directory 0700, the file 0600 via a
// temporary file and rename. Secret values and S3 object bodies that are
// already in the cloud are dropped first (see redactSpent).
func (j *Job) Save() error {
	dir, err := jobsDir()
	if err != nil {
		return err
	}
	if err := config.MkdirPrivate(dir); err != nil { // job files hold secret values of the manifest
		return err
	}
	// Directories made by older versions were 0755: job names and
	// manifests readable by every local user.
	if st, err := os.Stat(dir); err == nil && config.PermissionsEnforced() && config.LooseMode(st.Mode()) {
		_ = os.Chmod(dir, 0o700)
	}
	j.redactSpent()
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFilePrivate(filepath.Join(dir, j.ID+".json"), data)
}

// redactedValue replaces a secret value or object body on disk.
const redactedValue = "<redacted: not kept on disk after the step>"

// redactSpent drops the value of every secret and the body of every S3
// object whose step is done, or all of them once the job is destroyed: the
// job file would otherwise keep them in plain text forever. Paths, buckets
// and keys stay: stack_destroy deletes by them. Values of steps still to run
// are kept, stack_apply needs them.
func (j *Job) redactSpent() {
	gone := j.Status == "destroyed" || j.Status == "destroy_failed"
	spent := func(key string) bool {
		if gone {
			return true
		}
		for _, st := range j.Steps {
			if st.Key == key {
				return st.Status == "done"
			}
		}
		return false
	}
	for _, st := range j.Steps {
		if !spent(st.Key) {
			continue
		}
		switch st.Tool {
		case "secret_create":
			if _, ok := st.Args["value"]; ok {
				st.Args["value"] = redactedValue
			}
		case "s3_put":
			if _, ok := st.Args["content"]; ok {
				st.Args["content"] = redactedValue
			}
		}
	}
	for i := range j.Manifest.Secrets {
		if spent(fmt.Sprintf("secret:%d", i)) && j.Manifest.Secrets[i].Value != "" {
			j.Manifest.Secrets[i].Value = redactedValue
		}
	}
	for i := range j.Manifest.S3 {
		if spent(fmt.Sprintf("s3:%d", i)) && j.Manifest.S3[i].Content != "" {
			j.Manifest.S3[i].Content = redactedValue
		}
	}
}

// jobIDRe matches the ids newID makes. The id comes from the caller (MCP
// job_id) and becomes a file name: "../x" must not leave jobsDir.
var jobIDRe = regexp.MustCompile(`^stk-[0-9a-f]{10}$`)

// uuidRe matches a UUID (vm.image by id goes to provision as image_id).
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// LockJob takes the job's file lock, across processes: two MCP servers
// (stdio and HTTP) on one stack-jobs directory would otherwise both see a
// step pending and create it twice. It waits up to maxWait.
func LockJob(id string, maxWait time.Duration) (unlock func(), err error) {
	if !jobIDRe.MatchString(id) {
		return nil, fmt.Errorf(i18n.T("invalid job_id %q (want stk-<10 hex digits>, as stack_plan returns)",
			"неверный job_id %q (нужен stk-<10 шестнадцатеричных цифр>, как его возвращает stack_plan)"), id)
	}
	dir, err := jobsDir()
	if err != nil {
		return nil, err
	}
	unlock, err = state.FileLock(filepath.Join(dir, id+".lock"), maxWait)
	if errors.Is(err, state.ErrFileLocked) {
		return nil, fmt.Errorf(i18n.T("job %s is being changed by another evo process (another MCP server?); try again when it finishes",
			"задание %s меняет другой процесс evo (другой MCP-сервер?); повторите, когда он закончит"), id)
	}
	if err != nil {
		return nil, fmt.Errorf(i18n.T("lock job %s: %w", "блокировка задания %s: %w"), id, err)
	}
	return unlock, nil
}

// LoadJob reads a job from disk.
func LoadJob(id string) (*Job, error) {
	if !jobIDRe.MatchString(id) {
		return nil, fmt.Errorf(i18n.T("invalid job_id %q (want stk-<10 hex digits>, as stack_plan returns)",
			"неверный job_id %q (нужен stk-<10 шестнадцатеричных цифр>, как его возвращает stack_plan)"), id)
	}
	dir, err := jobsDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if os.IsNotExist(err) {
		// Without the text of the system: on Windows it is English in a
		// Russian message ("The system cannot find the file specified").
		return nil, &jobNotFoundError{id: id, err: err}
	}
	if err != nil {
		return nil, fmt.Errorf(i18n.T("job %s not found: %w", "задание %s не найдено: %w"), id, err)
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf(i18n.T("parse job %s: %w", "разбор задания %s: %w"), id, err)
	}
	return &j, nil
}

// --- Planning: manifest → ordered steps ---

// Plan builds a Job (steps + deps) from a manifest. Nothing is created yet.
func Plan(m Manifest) *Job {
	j := &Job{ID: newID(), Manifest: m, Status: "planned", Outputs: map[string]string{}}

	if pg := m.Postgres; pg != nil {
		args := map[string]any{"name": pg.Name, "confirmed": true}
		if pg.SpecID != "" {
			args["specification_id"] = pg.SpecID
		}
		if pg.CPU > 0 {
			args["cpu"] = pg.CPU
		}
		if pg.RAM > 0 {
			args["ram"] = pg.RAM
		}
		if pg.StorageGB > 0 {
			args["storage_gb"] = pg.StorageGB
		}
		if pg.NoBackup {
			args["no_backup"] = true
		}
		if pg.InitialDBName != "" {
			args["initial_database"] = pg.InitialDBName
		}
		j.Steps = append(j.Steps, &Step{
			Key: "postgres", Tool: "pg_create", Args: args,
			ReadyKind: "status", Status: "pending",
			StatusTool: "pg_get", StatusArg: "cluster_id", StatusField: "status", ReadyValue: "READY",
		})
	}

	if k := m.Kafka; k != nil {
		args := map[string]any{"name": k.Name, "confirmed": true}
		if k.VersionID != "" {
			args["version_id"] = k.VersionID
		}
		if k.SpecID != "" {
			args["specification_id"] = k.SpecID
		}
		if k.CPU > 0 {
			args["cpu"] = k.CPU
		}
		if k.RAM > 0 {
			args["ram"] = k.RAM
		}
		if k.StorageGiB > 0 {
			args["storage_gib"] = k.StorageGiB
		}
		if k.Brokers > 0 {
			args["brokers"] = k.Brokers
		}
		if k.UIEnabled {
			args["ui_enabled"] = true
		}
		j.Steps = append(j.Steps, &Step{
			Key: "kafka", Tool: "kafka_create", Args: args,
			ReadyKind: "status", Status: "pending",
			StatusTool: "kafka_get", StatusArg: "cluster_id", StatusField: "status", ReadyValue: "READY",
		})
	}

	if r := m.Redis; r != nil {
		args := map[string]any{"name": r.Name, "confirmed": true}
		if r.VersionID != "" {
			args["version_id"] = r.VersionID
		}
		if r.SpecID != "" {
			args["specification_id"] = r.SpecID
		}
		if r.CPU > 0 {
			args["cpu"] = r.CPU
		}
		if r.RAM > 0 {
			args["ram"] = r.RAM
		}
		if r.StorageGB > 0 {
			args["storage_gb"] = r.StorageGB
		}
		if r.Shards > 0 {
			args["shards"] = r.Shards
		}
		if r.Replicas > 0 {
			args["replicas"] = r.Replicas
		}
		if r.Type != "" {
			args["type"] = r.Type
		}
		j.Steps = append(j.Steps, &Step{
			Key: "redis", Tool: "redis_create", Args: args,
			ReadyKind: "status", Status: "pending",
			StatusTool: "redis_get", StatusArg: "cluster_id", StatusField: "status", ReadyValue: "READY",
		})
	}

	if v := m.VPC; v != nil {
		args := map[string]any{"name": v.Name, "confirmed": true}
		if v.Description != "" {
			args["description"] = v.Description
		}
		j.Steps = append(j.Steps, &Step{
			Key: "vpc", Tool: "vpc_create", Args: args,
			ReadyKind: "op", PollTool: "vpc_operation",
			Status: "pending",
		})
	}

	if k := m.K8s; k != nil {
		args := map[string]any{"name": k.Name, "confirmed": true}
		if k.Version != "" {
			args["version"] = k.Version
		}
		if k.Flavor != "" {
			args["flavor"] = k.Flavor
		}
		if k.CPU > 0 {
			args["cpu"] = k.CPU
		}
		if k.RAM > 0 {
			args["ram_gb"] = k.RAM
		}
		if k.MasterCount > 0 {
			args["master_count"] = k.MasterCount
		}
		if k.KubeAPIInternet {
			args["kube_api_internet"] = true
		}
		// k8s_create returns cluster_id directly; readiness = k8s_get status RUNNING.
		j.Steps = append(j.Steps, &Step{
			Key: "k8s", Tool: "k8s_create", Args: args,
			ReadyKind: "status", Status: "pending",
			StatusTool: "k8s_get", StatusArg: "cluster_id", StatusField: "status", ReadyValue: "RUNNING",
		})
	}

	if r := m.Registry; r != nil {
		args := map[string]any{"name": r.Name, "confirmed": true}
		if r.Type != "" {
			args["registry_type"] = r.Type
		}
		if r.Public {
			args["public"] = true
		}
		if r.Tariff != "" {
			args["tariff"] = r.Tariff
		}
		j.Steps = append(j.Steps, &Step{
			Key: "registry", Tool: "ar_create", Args: args,
			ReadyKind: "op", PollTool: "ar_operation",
			Status: "pending",
		})
	}

	if sn := m.SNAT; sn != nil {
		args := map[string]any{"name": sn.Name, "confirmed": true}
		if sn.Zone != "" {
			args["zone"] = sn.Zone
		}
		// snat_create is synchronous (returns id immediately).
		j.Steps = append(j.Steps, &Step{
			Key: "snat", Tool: "snat_create", Args: args,
			ReadyKind: "", Status: "pending",
		})
	}

	for i, sc := range m.Secrets {
		j.Steps = append(j.Steps, &Step{
			Key:       fmt.Sprintf("secret:%d", i),
			Tool:      "secret_create",
			Args:      map[string]any{"path": sc.Path, "value": sc.Value, "confirmed": true},
			ReadyKind: "", Status: "pending",
		})
	}

	for i, s3 := range m.S3 {
		j.Steps = append(j.Steps, &Step{
			Key:       fmt.Sprintf("s3:%d", i),
			Tool:      "s3_put",
			Args:      map[string]any{"bucket": s3.Bucket, "key": s3.Key, "content": s3.Content, "confirmed": true},
			ReadyKind: "", Status: "pending",
		})
	}

	if vm := m.VM; vm != nil {
		// A fixed name: provision names an unnamed VM "evo-<time>" on each
		// call, and a retry after a lost answer could not find it by name.
		name := vm.Name
		if name == "" {
			name = m.Name + "-vm"
		}
		args := map[string]any{"name": name, "confirmed": true}
		if vm.Private {
			args["no_public_ip"] = true
		}
		if vm.Flavor != "" {
			args["flavor"] = vm.Flavor
		}
		// image_id wins over the server's CLOUDRU_IMAGE_ID, image (a name)
		// would not; region is the zone UUID provision takes.
		if vm.Image != "" {
			if uuidRe.MatchString(vm.Image) {
				args["image_id"] = vm.Image
			} else {
				args["image"] = vm.Image
			}
		}
		if vm.Zone != "" {
			args["region"] = vm.Zone
		}
		// provision is synchronous (waits for active), returns vm_id. A
		// failed answer may still have created the VM: it is looked up by
		// name before any retry, as for the other steps.
		j.Steps = append(j.Steps, &Step{
			Key: "vm", Tool: "provision", Args: args, ReadyKind: "", Status: "pending",
		})
	}

	if nlb := m.NLB; nlb != nil {
		target := nlb.Target
		if target == "" {
			target = "vm"
		}
		// tg_create auto-resolves interface_id from vm_id.
		j.Steps = append(j.Steps, &Step{
			Key: "tg", Tool: "tg_create",
			Args:      map[string]any{"name": nlb.Name + "-tg", "vm_id": "$" + target, "confirmed": true},
			DependsOn: []string{target},
			PollTool:  "lb_operation", ReadyKind: "op",
			Status: "pending",
		})
		lbArgs := map[string]any{
			"name": nlb.Name, "target_group_id": "$tg", "port": nlb.Port,
			"external": nlb.External, "confirmed": true,
		}
		if nlb.TargetPort > 0 {
			lbArgs["target_port"] = nlb.TargetPort
		}
		j.Steps = append(j.Steps, &Step{
			Key: "lb", Tool: "lb_create", Args: lbArgs,
			DependsOn: []string{"tg"},
			PollTool:  "lb_operation", ReadyKind: "op",
			Status: "pending",
		})
	}

	if d := m.DNS; d != nil {
		rtype := d.RecordType
		if rtype == "" {
			rtype = "a"
		}
		ttl := d.TTL
		if ttl == 0 {
			ttl = 300
		}
		// value = the LB's external IP (resolved from outputs at run time) unless
		// an explicit literal target is given.
		value := d.Target
		deps := []string(nil)
		if value == "" || value == "lb" {
			value = "$lb_external_ip"
			deps = []string{"lb"}
		}
		j.Steps = append(j.Steps, &Step{
			Key: "dns", Tool: "dns_record_create",
			Args: map[string]any{
				"zone_id": d.ZoneID, "name": d.Name, "record_type": rtype,
				"value": value, "ttl": ttl, "confirmed": true,
			},
			DependsOn: deps,
			PollTool:  "dns_operation", ReadyKind: "op",
			Status: "pending",
		})
	}

	return j
}

// CheckDeps reports a step that depends on a step the job does not have, or
// a dependency cycle. Such a step never starts: the job would create the
// other resources and then stay "running" forever.
func (j *Job) CheckDeps() error {
	byKey := make(map[string]*Step, len(j.Steps))
	for _, st := range j.Steps {
		byKey[st.Key] = st
	}
	for _, st := range j.Steps {
		for _, d := range st.DependsOn {
			if byKey[d] == nil {
				return fmt.Errorf(i18n.T("step %q depends on %q, which this stack does not have (nlb.target must name a step such as \"vm\"; dns without an explicit target needs nlb)",
					"шаг %q зависит от %q, которого в этом стеке нет (nlb.target должен называть шаг, например \"vm\"; dns без явной цели требует nlb)"), st.Key, d)
			}
		}
	}
	const (
		unseen = iota
		inPath
		finished
	)
	state := map[string]int{}
	var visit func(key string, path []string) error
	visit = func(key string, path []string) error {
		switch state[key] {
		case inPath:
			return fmt.Errorf(i18n.T("dependency cycle: %s", "цикл зависимостей: %s"), strings.Join(append(path, key), " → "))
		case finished:
			return nil
		}
		state[key] = inPath
		for _, d := range byKey[key].DependsOn {
			if err := visit(d, append(path, key)); err != nil {
				return err
			}
		}
		state[key] = finished
		return nil
	}
	for _, st := range j.Steps {
		if err := visit(st.Key, nil); err != nil {
			return err
		}
	}
	return nil
}

// Rate-card monthly estimates (RUB) — NOT a bill; exact cost via billing_finops
// after apply. Overridable later via config if needed.
const (
	rateNLBMonthly      = 2900.0
	rateVMLowMonthly    = 800.0  // lowcost 1 vCPU
	rateVMDefault       = 1700.0 // ~2 vCPU
	ratePGvCPUMonthly   = 4900.0 // per vCPU, standard tier w/ backups
	rateDNSZoneMonthly  = 0.0    // a record in an existing zone ~ free
	rateK8sMonthly      = 5000.0 // small control plane + a node
	rateKafkaMonthly    = 4900.0 // small cluster
	rateRedisMonthly    = 3000.0 // small standalone
	rateRegistryMonthly = 500.0  // BASIC tariff
	rateSNATMonthly     = 1000.0
	rateVPCMonthly      = 0.0 // VPC itself is free
)

// EstimateCost returns a rough rate-card monthly estimate for a manifest.
func EstimateCost(m Manifest) map[string]any {
	var total float64
	comps := []map[string]any{}
	add := func(name string, rub float64) {
		comps = append(comps, map[string]any{"component": name, "monthly_rub": rub})
		total += rub
	}
	if pg := m.Postgres; pg != nil {
		cpu := pg.CPU
		if cpu <= 0 {
			cpu = 1
		}
		add("postgres", ratePGvCPUMonthly*float64(cpu))
	}
	if vm := m.VM; vm != nil {
		c := rateVMDefault
		if vm.Flavor == "" || (len(vm.Flavor) >= 12 && vm.Flavor[:12] == "lowcost10-1-") {
			c = rateVMLowMonthly
		}
		add("vm", c)
	}
	if m.NLB != nil {
		add("nlb", rateNLBMonthly)
	}
	if m.VPC != nil {
		add("vpc", rateVPCMonthly)
	}
	if k := m.K8s; k != nil {
		c := rateK8sMonthly
		if k.CPU > 2 {
			c += ratePGvCPUMonthly * 0 // control-plane scaling folded into base for a rough estimate
		}
		add("k8s", c)
	}
	if m.Kafka != nil {
		add("kafka", rateKafkaMonthly)
	}
	if m.Redis != nil {
		add("redis", rateRedisMonthly)
	}
	if m.Registry != nil {
		add("registry", rateRegistryMonthly)
	}
	if m.SNAT != nil {
		add("snat", rateSNATMonthly)
	}
	if m.DNS != nil {
		add("dns", rateDNSZoneMonthly)
	}
	for range m.Secrets {
		add("secret", 0)
	}
	for range m.S3 {
		add("s3", 0)
	}
	return map[string]any{
		"currency":          "RUB",
		"total_monthly_rub": total,
		"by_component":      comps,
		"note":              "rate-card estimate (не биллинг); точное — billing_finops после apply",
	}
}

// Compact returns a small status view for the agent (no per-step payloads).
func (j *Job) Compact() map[string]any {
	var done, pending, running []string
	for _, s := range j.Steps {
		switch s.Status {
		case "done":
			done = append(done, s.Key)
		case "running", "waiting":
			running = append(running, s.Key)
		default:
			pending = append(pending, s.Key)
		}
	}
	out := map[string]any{
		"job_id":  j.ID,
		"status":  j.Status,
		"done":    done,
		"running": running,
		"pending": pending,
		"outputs": j.Outputs,
	}
	if j.ProjectID != "" {
		out["project_id"] = j.ProjectID
	}
	if j.Error != "" {
		out["error"] = j.Error
	}
	// What a step met and how many creates it used: a step that stays
	// pending or ends in error says why (G130-04, D41); one in error also
	// says the class of its tool's refusal, so an agent tells what it can
	// fix without reading the text (G140-5). Not "reason": that key is the
	// error answer's (TestParity_ErrorReasonEqualsTool), and this answer is
	// a status.
	details, attempts, reasons := map[string]string{}, map[string]int{}, map[string]string{}
	for _, s := range j.Steps {
		if s.Detail != "" && s.Status != "done" {
			details[s.Key] = s.Detail
		}
		if s.Attempts > 0 {
			attempts[s.Key] = s.Attempts
		}
		if s.Reason != "" && s.Status == "error" {
			reasons[s.Key] = s.Reason
		}
	}
	if len(details) > 0 {
		out["details"] = details
	}
	if len(reasons) > 0 {
		out["error_reasons"] = reasons
	}
	if len(attempts) > 0 {
		out["attempts"] = attempts
	}
	return out
}

// jobNotFoundError is a job file that does not exist. The file error stays
// reachable through Unwrap.
type jobNotFoundError struct {
	id  string
	err error
}

func (e *jobNotFoundError) Error() string {
	return fmt.Sprintf(i18n.T("job %s not found in ~/.evo/stack-jobs (stack_plan returns the job_id)",
		"задание %s не найдено в ~/.evo/stack-jobs (job_id возвращает stack_plan)"), e.id)
}

func (e *jobNotFoundError) Unwrap() error { return e.err }
