// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// --- stack orchestrator: crank-driven, disk-persisted multi-resource deploys ---
//
// The heavy execution loop lives HERE (server-side), not in the agent's context.
// The agent calls stack_apply once, then polls stack_status; each poll is one
// "crank" that advances the persisted state machine as far as it can and returns
// a COMPACT status. Agent context stays O(1) regardless of stack size — no
// trajectory/context overflow, LLM-agnostic, resumable after context loss.

// callTool invokes an existing MCP tool handler in-process and parses its JSON.
// This reuses every typed operation (pg_create, provision, tg_create, …) with
// zero duplication — the orchestrator drives the same tools the agent would.
func (s *Registry) callTool(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	h, ok := s.handlers[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	// The step is not the CLI command's call (Caller): its progress and
	// its error are the outer tool's to show.
	text, isErr := h(WithCaller(ctx, nil), args)
	var out map[string]any
	if e := json.Unmarshal([]byte(text), &out); e != nil {
		return nil, fmt.Errorf("%s: unparseable result: %s", name, text)
	}
	if isErr {
		msg, _ := out["error"].(string)
		if msg == "" {
			msg = text
		}
		return out, fmt.Errorf("%s: %s", name, msg)
	}
	return out, nil
}

// substituteRefs replaces "$stepkey" string arg values with the produced output.
func substituteRefs(args map[string]any, outputs map[string]string) map[string]any {
	res := make(map[string]any, len(args))
	for k, v := range args {
		if sv, ok := v.(string); ok && len(sv) > 1 && sv[0] == '$' {
			if id, ok := outputs[sv[1:]]; ok {
				res[k] = id
				continue
			}
		}
		res[k] = v
	}
	return res
}

func depsDone(step *stack.Step, job *stack.Job) bool {
	for _, d := range step.DependsOn {
		found := false
		for _, s := range job.Steps {
			if s.Key == d {
				found = s.Status == "done"
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// findIDByName resolves a resource id from a list tool by matching name. extra
// carries any params the list tool needs (e.g. dns_records needs zone_id).
func (s *Registry) findIDByName(ctx context.Context, tool, field, name string, extra map[string]any) string {
	args := map[string]any{}
	for k, v := range extra {
		args[k] = v
	}
	res, err := s.callTool(ctx, tool, args)
	if err != nil {
		return ""
	}
	arr, _ := res[field].([]any)
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			// A deleted namesake is not the step's resource (as takenName).
			if st := strings.ToUpper(fmt.Sprint(m["status"])); strings.Contains(st, "DELETED") || strings.Contains(st, "ERASED") {
				continue
			}
			if n, _ := m["name"].(string); n == name {
				id, _ := m["id"].(string)
				return id
			}
		}
	}
	return ""
}

// takenByName is the id of a live resource named as the step's, from its
// list tool; an error when the list could not be read (then nothing is
// known and nothing may be created yet).
func (s *Registry) takenByName(ctx context.Context, st *stack.Step) (string, error) {
	args := map[string]any{}
	for k, v := range st.FindArgs {
		args[k] = v
	}
	res, err := s.callTool(ctx, st.FindTool, args)
	if err != nil {
		return "", err
	}
	arr, ok := res[st.FindField].([]any)
	if !ok {
		return "", fmt.Errorf("%s answered without %q", st.FindTool, st.FindField)
	}
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); n != st.FindName {
			continue
		}
		if status := strings.ToUpper(fmt.Sprint(m["status"])); strings.Contains(status, "DELETED") || strings.Contains(status, "ERASED") {
			continue
		}
		id, _ := m["id"].(string)
		return firstNonEmptyStr(id, st.FindName), nil
	}
	return "", nil
}

// vmClockSkew is how much earlier than the step's start a VM's creation time
// may read and still be the step's own (cloud and local clocks differ).
const vmClockSkew = 2 * time.Minute

// findStepResource looks up what a step may have created when its create
// answer is unknown. A VM (provision) is the step's own only when its name
// matches AND it was created after the step started: the name alone could
// be a user's VM, which stack_destroy would then delete (QA R3/R8 class).
// state is the VM's state ("" for other kinds). Steps that never started,
// or VMs without a creation time, are not adopted.
func (s *Registry) findStepResource(ctx context.Context, st *stack.Step) (id, state string) {
	if st.FindTool == "" || st.FindName == "" {
		return "", ""
	}
	if st.Tool != "provision" {
		return s.findIDByName(ctx, st.FindTool, st.FindField, st.FindName, st.FindArgs), ""
	}
	if st.StartedAt.IsZero() {
		return "", ""
	}
	res, err := s.callTool(ctx, st.FindTool, st.FindArgs)
	if err != nil {
		return "", ""
	}
	arr, _ := res[st.FindField].([]any)
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); n != st.FindName {
			continue
		}
		created, _ := m["created_time"].(string)
		t, err := time.Parse(time.RFC3339, created)
		if err != nil || t.Before(st.StartedAt.Add(-vmClockSkew)) {
			continue
		}
		vid, _ := m["id"].(string)
		vstate, _ := m["state"].(string)
		return vid, vstate
	}
	return "", ""
}

// adoptVM finishes a provision step with a VM found by findStepResource: an
// active VM completes the step; any other state (creating, error_creating)
// is recorded for stack_destroy and stops the job — it is billed, and
// marking it done would hand a broken VM to the steps after it.
func (s *Registry) adoptVM(ctx context.Context, job *stack.Job, st *stack.Step, id, state string) {
	switch strings.ToLower(state) {
	case "running", "active":
		s.finalizeStep(ctx, job, st, id)
	default:
		st.ResourceID = id
		st.Status = "error"
		job.Error = fmt.Sprintf("%s: VM %s found in state %q (kept for stack_destroy)", st.Key, id, state)
		job.Status = "error"
	}
}

// maxStepAttempts bounds create retries per step (transient-error tolerance).
const maxStepAttempts = 4

// stackCrankInterval is the pause between cranks inside one stack_status
// call. A variable only so that tests can shorten it.
var stackCrankInterval = 8 * time.Second

// isTerminalFail reports whether a resource status means it will never become
// ready (a failed create) — so the orchestrator stops waiting and fails the job
// instead of polling forever. Covers pg/kafka/redis (FAILED/ERASED) and k8s.
func isTerminalFail(status string) bool {
	u := strings.ToUpper(status)
	return strings.Contains(u, "FAIL") || strings.Contains(u, "ERROR") || strings.Contains(u, "ERASED")
}

// finalizeStep transitions a step to its resolved state given a resource id that
// was either just created or ADOPTED during reconcile (create response failed but
// the resource exists). ReadyKind decides whether it's done or still needs a wait.
func (s *Registry) finalizeStep(ctx context.Context, job *stack.Job, st *stack.Step, id string) {
	st.ResourceID = id
	switch st.ReadyKind {
	case "status":
		st.Status = "waiting" // resource exists; poll its status on later cranks
	case "op":
		job.Outputs[st.Key] = id
		if st.Key == "lb" && id != "" {
			if lb, e := s.callTool(ctx, "lb_get", map[string]any{"id": id}); e == nil {
				if ext, ok := lb["external_address"].(map[string]any); ok {
					if ip, _ := ext["ipv4"].(string); ip != "" {
						job.Outputs["lb_external_ip"] = ip
					}
				}
			}
		}
		st.Status = "done"
	default:
		job.Outputs[st.Key] = id
		st.Status = "done"
	}
}

// advance is ONE crank: poll in-flight async steps and start ready pending steps,
// looping until no further progress is possible this turn. Persists nothing (the
// caller saves). Returns quickly — long async waits (PG READY) span cranks.
func (s *Registry) advance(ctx context.Context, job *stack.Job) {
	for progress := true; progress; {
		progress = false
		for _, st := range job.Steps {
			if ctx.Err() != nil {
				// The call was cancelled: every further tool call would fail
				// on the context, burn an attempt and be reported as a cloud
				// error. The job is saved as it is; the next crank goes on.
				return
			}
			switch st.Status {
			case "done", "error":
				continue

			case "waiting":
				if st.ReadyKind == "op" {
					if st.OpID == "" {
						st.Status = "error"
						job.Error = st.Key + ": missing operation id"
						job.Status = "error"
						return
					}
					op, err := s.callTool(ctx, st.PollTool, map[string]any{"operation_id": st.OpID})
					if err != nil {
						// Transient poll failure — don't kill the job; retry next crank.
						st.Detail = "poll: " + err.Error()
						continue
					}
					if done, _ := op["done"].(bool); done {
						// A completed operation can still carry an error (create
						// failed server-side). Treat that as terminal, don't proceed.
						if oe, ok := op["error"].(map[string]any); ok && oe != nil {
							msg, _ := oe["message"].(string)
							st.Status = "error"
							job.Error = fmt.Sprintf("%s: operation failed: %v", st.Key, msg)
							job.Status = "error"
							return
						}
						id := s.findIDByName(ctx, st.FindTool, st.FindField, st.FindName, st.FindArgs)
						st.ResourceID = id
						job.Outputs[st.Key] = id
						if st.Key == "lb" && id != "" {
							if lb, e := s.callTool(ctx, "lb_get", map[string]any{"id": id}); e == nil {
								if ext, ok := lb["external_address"].(map[string]any); ok {
									if ip, _ := ext["ipv4"].(string); ip != "" {
										job.Outputs["lb_external_ip"] = ip
									}
								}
							}
						}
						st.Status = "done"
						progress = true
					}
				} else if st.ReadyKind == "status" {
					if st.ResourceID == "" {
						st.ResourceID = s.findIDByName(ctx, st.FindTool, st.FindField, st.FindName, st.FindArgs)
					}
					if st.ResourceID != "" {
						cl, err := s.callTool(ctx, st.StatusTool, map[string]any{st.StatusArg: st.ResourceID})
						if err == nil {
							status, _ := cl[st.StatusField].(string)
							if status == "" {
								status, _ = cl["status"].(string)
							}
							switch {
							case status != "" && strings.Contains(status, st.ReadyValue):
								job.Outputs[st.Key] = st.ResourceID
								st.Status = "done"
								progress = true
							case isTerminalFail(status):
								// FAILED/ERROR/ERASED — never reaches READY; fail the
								// job instead of polling forever.
								st.Status = "error"
								job.Error = fmt.Sprintf("%s: cluster reached terminal status %s", st.Key, status)
								job.Status = "error"
								return
							default:
								st.Detail = "status=" + status
							}
						}
					}
				}

			case "pending":
				if !depsDone(st, job) {
					continue
				}
				resumed := st.Interrupted
				if st.Interrupted {
					// The previous create was cut off and may have reached the
					// cloud: adopt what it made instead of creating it twice.
					id, state := st.ResourceID, ""
					if id == "" {
						id, state = s.findStepResource(ctx, st)
					}
					if ctx.Err() != nil {
						return
					}
					st.Interrupted = false
					if id != "" {
						if st.Tool == "provision" && state != "" {
							s.adoptVM(ctx, job, st, id, state)
							if job.Status == "error" {
								return
							}
						} else {
							s.finalizeStep(ctx, job, st, id)
						}
						st.Detail = "adopted after an interrupted create"
						progress = true
						continue
					}
				}
				if st.StartedAt.IsZero() && !resumed && st.Tool != "provision" && st.FindTool != "" && st.FindName != "" {
					// Before the step's first create: a resource with its
					// name now is not the job's. Without this, a retry after
					// an attempt that failed before its create found it by
					// name below and adopted it — stack_destroy would delete
					// a user's cluster (final audit, stage 1). Whatever the
					// name finds after this point the step made itself. A
					// VM is matched by its creation time instead.
					id, err := s.takenByName(ctx, st)
					if err != nil {
						st.Detail = "check the name is free: " + err.Error()
						continue // pending: the next pass checks again
					}
					if id != "" {
						st.Status = "error"
						job.Error = fmt.Sprintf("%s: %q is already taken by an existing resource (%s) — rename it in the manifest, or remove the existing one", st.Key, st.FindName, id)
						job.Status = "error"
						return
					}
				}
				if st.StartedAt.IsZero() {
					st.StartedAt = time.Now()
				}
				args := substituteRefs(st.Args, job.Outputs)
				// stack_apply was confirmed for the whole stack. Jobs planned
				// before provision and s3_put got their gates lack the flag.
				args["confirmed"] = true
				res, err := s.callTool(ctx, st.Tool, args)
				if err != nil && ctx.Err() != nil {
					// Cancelled mid-create: not a failed attempt. Keep the id
					// the tool reported (provision names the VM it made).
					st.Interrupted = true
					st.ResourceID = firstStr(res, "vm_id", "cluster_id", "id")
					st.Detail = "interrupted: " + err.Error()
					return
				}
				if err != nil {
					st.Attempts++
					st.Detail = err.Error()
					// The tool named what it made although the call failed
					// (provision: a VM in error_creating or not active in
					// time). It exists and is billed: record it for
					// stack_destroy and stop — a retry would create another.
					if id := firstStr(res, "vm_id", "cluster_id"); id != "" {
						st.ResourceID = id
						st.Status = "error"
						job.Error = fmt.Sprintf("%s: %s (resource %s kept for stack_destroy)", st.Key, err.Error(), id)
						job.Status = "error"
						return
					}
					// The tool refused a name a resource already has
					// (existing_id; release audit, L-32). Unless the step
					// resumes after a cut-off create, that resource is not
					// known to be the job's: adopting it by name below would
					// hand it to stack_destroy. Only on the first try it was
					// refused; a retry after an attempt that failed before
					// its create adopted a user's cluster the same way
					// (final audit, stage 1). An attempt whose create did go
					// through with its answer lost leaves its resource
					// named here, for the user to keep or delete.
					if ex := firstStr(res, "existing_id"); ex != "" && !resumed {
						st.Status = "error"
						job.Error = fmt.Sprintf("%s: %s — rename it in the manifest, or remove the existing one (%s); "+
							"if an earlier attempt of this step created it, delete it or keep it: the stack does not adopt it by name", st.Key, err.Error(), ex)
						job.Status = "error"
						return
					}
					// Reconcile before retrying: a create can fail the RESPONSE yet
					// succeed server-side (e.g. 504 gateway timeout after the backend
					// applied it). If the resource now exists by name, ADOPT it — never
					// blindly retry a create (that would double-provision).
					if id, state := s.findStepResource(ctx, st); id != "" {
						if st.Tool == "provision" {
							s.adoptVM(ctx, job, st, id, state)
							if job.Status == "error" {
								return
							}
						} else {
							s.finalizeStep(ctx, job, st, id)
						}
						progress = true
						continue
					}
					// Genuinely not created → retry on a later crank, bounded.
					if st.Attempts >= maxStepAttempts {
						st.Status = "error"
						job.Error = fmt.Sprintf("%s: %s (after %d attempts)", st.Key, err.Error(), st.Attempts)
						job.Status = "error"
						return
					}
					continue // stay pending, no progress → retried next stack_status crank
				}
				if st.ReadyKind == "" {
					// synchronous — capture id and finish
					id := firstStr(res, "vm_id", "id", "path")
					if id != "" {
						job.Outputs[st.Key] = id
						st.ResourceID = id
					} else {
						job.Outputs[st.Key] = "ok"
					}
					st.Status = "done"
				} else {
					// async — remember the op and wait on subsequent cranks
					st.OpID = firstStr(res, "operation_id")
					if st.ReadyKind == "status" {
						// some creates return the id directly (k8s: cluster_id)
						st.ResourceID = firstStr(res, "cluster_id", "id")
					}
					st.Status = "waiting"
				}
				progress = true
			}
		}
	}

	// terminal check
	if job.Status == "error" {
		return
	}
	allDone := true
	for _, st := range job.Steps {
		if st.Status != "done" {
			allDone = false
			break
		}
	}
	if allDone {
		job.Status = "done"
	} else {
		job.Status = "running"
	}
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// --- handlers ---

func (s *Registry) handleStackPlan(ctx context.Context, args map[string]any) (string, bool) {
	raw, ok := args["manifest"]
	if !ok {
		return usageT("'manifest' is required (object describing the stack)", "нужен 'manifest' — объект с описанием стека"), true
	}
	b, _ := json.Marshal(raw)
	var m stack.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return errResult(fmt.Errorf(i18n.T("invalid manifest: %w", "неверный манифест: %w"), err)), true
	}
	if m.Name == "" {
		return usageT("manifest.name is required", "нужен manifest.name"), true
	}
	// Validate + normalize the manifest BEFORE planning, so a malformed field
	// fails fast at plan time (nothing created) instead of wedging the stack in
	// "error" mid-deploy after other resources are already live.
	s3Notes, err := s.normalizeManifest(ctx, &m)
	if err != nil {
		return errResult(err), true
	}
	var vmNote map[string]any
	if m.VM != nil {
		if vmNote, err = s.normalizeStackVM(ctx, m.VM); err != nil {
			return errResult(err), true
		}
	}
	// Redis shape and Kafka brokers fail at plan time, like the VM: a stack
	// with redis.replicas=3 (the old documented default) used to plan and
	// then stop at the redis step with vpc, pg, kafka and k8s already live
	// (Grok review of PR #89).
	if r := m.Redis; r != nil {
		ra := map[string]any{}
		if r.Type != "" {
			ra["type"] = r.Type
		}
		if r.Shards != 0 {
			ra["shards"] = r.Shards
		}
		if r.Replicas != 0 {
			ra["replicas"] = r.Replicas
		}
		if _, _, _, err := redisShape(ra); err != nil {
			return usageResult(fmt.Errorf("manifest.redis: %w", err)), true
		}
	}
	if m.Kafka != nil && m.Kafka.Brokers < 0 {
		return usageT("manifest.kafka.brokers %d: must be 1 or more (or omitted)", "manifest.kafka.brokers %d: нужно 1 или больше (или не указывать)", m.Kafka.Brokers), true
	}
	if m.K8s != nil && m.K8s.MasterCount != 0 {
		if err := k8sArgsError(map[string]any{"master_count": m.K8s.MasterCount}); err != nil {
			return errResult(err), true
		}
	}
	job := stack.Plan(m)
	if eng, e := s.newEngine(nil); e == nil && eng != nil {
		job.ProjectID = eng.Config.ProjectID
	}
	if err := job.CheckDeps(); err != nil {
		return errResult(fmt.Errorf(i18n.T("invalid manifest: %w", "неверный манифест: %w"), err)), true
	}
	if err := job.Save(); err != nil {
		return errResult(fmt.Errorf("persist plan: %w", err)), true
	}
	steps := make([]string, 0, len(job.Steps))
	for _, st := range job.Steps {
		steps = append(steps, st.Key)
	}
	out := map[string]any{
		"status":     "planned",
		"job_id":     job.ID,
		"project_id": job.ProjectID,
		"steps":      steps,
		"cost":       stack.EstimateCost(m),
		"message":    "Stack planned. BILLABLE — show the user the cost and get explicit approval, then call stack_apply with this job_id and confirmed=true.",
	}
	if len(s3Notes) > 0 {
		out["s3"] = s3Notes
	}
	if vmNote != nil {
		out["vm"] = vmNote
	}
	// Warn if a prior job for this stack name still holds resources — applying now
	// would create duplicates (double billing). Point the user at stack_destroy.
	if orphans := stack.FindResourcefulJobs(m.Name, job.ID); len(orphans) > 0 {
		out["warning"] = fmt.Sprintf("%d earlier job(s) for stack %q still hold resources — applying THIS plan will create DUPLICATES. Run stack_destroy on them first.", len(orphans), m.Name)
		out["existing_jobs"] = orphans
	}
	return toJSON(out), false
}

// normalizeStackVM resolves the image and the zone of the stack's VM at
// plan time and writes their UUIDs into the manifest: vm.image (name or
// UUID, as provision's image), else the server's CLOUDRU_IMAGE_ID; vm.zone
// (name or UUID), else CLOUDRU_REGION. A missing, unknown or closed one is
// refused here, before anything is created: the vm step used to fail
// after the earlier steps were live (release audit, L-36). Returns what
// the plan shows for the VM.
func (s *Registry) normalizeStackVM(ctx context.Context, vm *stack.VMSpec) (map[string]any, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("validate vm: %w", err)
	}
	imageRef := firstNonEmptyStr(vm.Image, eng.Config.ImageID, eng.Config.Image)
	if imageRef == "" {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.T("manifest vm.image is required: the image of the VM, by name or UUID", "в манифесте нужен vm.image — образ ВМ, имя или UUID"),
			i18n.T("Set vm.image, e.g. \"Ubuntu-24.04\" (image_list); the server has no CLOUDRU_IMAGE_ID (an image UUID) to default to.", "Задайте vm.image, например \"Ubuntu-24.04\" (image_list): у сервера не задан CLOUDRU_IMAGE_ID (UUID образа по умолчанию)."))
	}
	cat := images.Catalog{Cloud: eng.Cloud, ProjectID: eng.Config.ProjectID}
	img, err := cat.Get(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("vm.image %q: %w", imageRef, err)
	}
	if img.NotInProject {
		return nil, images.NotInProjectError(img.Name)
	}

	zoneRef := firstNonEmptyStr(vm.Zone, eng.Config.Region)
	if zoneRef == "" {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.T("manifest vm.zone is required: the availability zone of the VM, by name or UUID", "в манифесте нужен vm.zone — зона доступности ВМ, имя или UUID"),
			i18n.T("Set vm.zone, e.g. \"ru.AZ-1\" (list_zones); the server has no CLOUDRU_REGION (a zone UUID) to default to.", "Задайте vm.zone, например \"ru.AZ-1\" (list_zones): у сервера не задан CLOUDRU_REGION (UUID зоны по умолчанию)."))
	}
	zones, err := eng.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("validate vm.zone: list availability zones: %w", err)
	}
	zone, err := provision.ResolveZone(zones, zoneRef)
	if err != nil {
		return nil, clierrors.Prefix("vm.zone: ", err)
	}
	if err := provision.ClosedZoneError(zones, zone.ID); err != nil {
		return nil, err
	}
	vm.Image, vm.Zone = img.ID, zone.ID
	return map[string]any{"image": firstNonEmptyStr(img.Name, img.ID), "image_id": img.ID,
		"zone": firstNonEmptyStr(zone.Name, zone.ID)}, nil
}

// normalizeManifest validates a stack manifest and fills in safe defaults BEFORE
// planning, so a malformed field fails fast at plan time (nothing is created)
// instead of wedging the whole stack in "error" mid-deploy — the failure mode
// that an empty s3 bucket produced (s3_put needs bucket+key → 4 retries → the
// job goes "error" though snat/secret/vm already deployed).
//
// It returns per-object s3 notes (the resolved bucket/key) for transparency, and
// only calls the S3 API when a bucket actually needs defaulting.
func (s *Registry) normalizeManifest(ctx context.Context, m *stack.Manifest) ([]string, error) {
	// --- VM flavor: validate at plan time so a bad name fails fast here rather
	// than mid-apply. A non-existent flavor (e.g. "performance-2-4") otherwise
	// passes planning, then the VM step 500s during apply — and by then earlier
	// steps (a Postgres cluster) are already live, leaving a billable orphan that
	// a re-plan duplicates. Catching it here prevents that whole chain.
	if m.VM != nil && m.VM.Flavor != "" && m.VM.Flavor != "auto" {
		eng, err := s.authedEngine(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("validate vm.flavor: %w", err)
		}
		flavors, err := eng.Cloud.ListFlavors(ctx)
		if err != nil {
			return nil, fmt.Errorf("validate vm.flavor: %w", err)
		}
		found := false
		names := make([]string, 0, len(flavors))
		for _, f := range flavors {
			if f.Name == m.VM.Flavor || f.ID == m.VM.Flavor {
				found = true
				break
			}
			names = append(names, f.Name)
		}
		if !found {
			return nil, fmt.Errorf(i18n.T("vm.flavor %q does not exist in this project. Pick a real one (see list_flavors) — available: %s", "vm.flavor %q в проекте нет. Выберите существующую конфигурацию (см. list_flavors) — доступны: %s"), m.VM.Flavor, strings.Join(names[:min(len(names), 14)], ", "))
		}
	}

	if len(m.S3) == 0 {
		return nil, nil
	}

	// Resolve the project bucket list only if at least one object omits it.
	var buckets []string
	fetched := false
	fetch := func() error {
		if fetched {
			return nil
		}
		cli, err := s.s3Client()
		if err != nil {
			return fmt.Errorf("resolve default S3 bucket: %w", err)
		}
		bs, err := cli.ListBuckets(ctx)
		if err != nil {
			return fmt.Errorf("resolve default S3 bucket: %w", err)
		}
		for _, b := range bs {
			buckets = append(buckets, b.Name)
		}
		fetched = true
		return nil
	}

	notes := make([]string, 0, len(m.S3))
	for i := range m.S3 {
		if m.S3[i].Key == "" {
			return nil, fmt.Errorf(i18n.T("s3[%d].key is required (the object path inside the bucket, e.g. \"config/app.yaml\")", "нужен s3[%d].key — путь объекта в бакете, например \"config/app.yaml\""), i)
		}
		if m.S3[i].Bucket == "" {
			if err := fetch(); err != nil {
				return nil, err
			}
			switch len(buckets) {
			case 0:
				return nil, fmt.Errorf(i18n.T("s3[%d].bucket is empty and this project has no S3 bucket — create one (s3_buckets to check) or set s3[%d].bucket", "s3[%d].bucket пуст, а в проекте нет бакета S3 — создайте бакет (проверка — s3_buckets) или задайте s3[%d].bucket"), i, i)
			case 1:
				m.S3[i].Bucket = buckets[0]
			default:
				return nil, fmt.Errorf(i18n.T("s3[%d].bucket is empty and this project has %d buckets %v — set s3[%d].bucket explicitly", "s3[%d].bucket пуст, а в проекте %d бакетов %v — задайте s3[%d].bucket"), i, len(buckets), buckets, i)
			}
		}
		notes = append(notes, fmt.Sprintf("%s/%s", m.S3[i].Bucket, m.S3[i].Key))
	}
	return notes, nil
}

func (s *Registry) handleStackApply(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "job_id")
	if id == "" {
		return usageT("'job_id' is required (from stack_plan)", "нужен 'job_id' (из stack_plan)"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.T("stack_apply provisions BILLABLE resources.",
			"stack_apply создаёт ПЛАТНЫЕ ресурсы.")}), true
	}
	unlock, err := s.lockJob(id)
	if err != nil {
		return errResult(err), true
	}
	defer unlock()
	job, err := stack.LoadJob(id)
	if err != nil {
		return errResult(err), true
	}
	if err := s.checkJobProject(job); err != nil {
		return errResult(err), true
	}
	if job.Status == "destroyed" || job.Status == "destroy_failed" {
		// Its steps are all "done" from the first apply: advancing would
		// report status=done for resources that no longer exist.
		return errResult(fmt.Errorf(i18n.T("job %s is %s: its resources were torn down; run stack_plan again for a new deploy", "задание %s в состоянии %s: его ресурсы удалены; для нового развёртывания снова выполните stack_plan"), id, job.Status)), true
	}
	s.advance(ctx, job)
	if e := job.Save(); e != nil {
		return errResult(fmt.Errorf("persist job: %w", e)), true
	}
	out := job.Compact()
	out["note"] = "Async — poll stack_status(job_id) until status=done. Each poll advances the deploy."
	return toJSON(out), job.Status == "error"
}

// lockJob serializes the stack tools on one job and returns the unlock.
// Each crank loads the job, starts the pending steps and saves it; two
// concurrent cranks (two HTTP clients, a poll retried while the first one
// still runs, a stdio and an HTTP server on one stack-jobs directory) would
// both see a step pending and create it twice. The mutex orders the calls
// of this process; the file lock, other processes.
func (s *Registry) lockJob(id string) (unlock func(), err error) {
	s.jobMu.Lock()
	if s.jobLocks == nil {
		s.jobLocks = map[string]*sync.Mutex{}
	}
	mu := s.jobLocks[id]
	if mu == nil {
		mu = &sync.Mutex{}
		s.jobLocks[id] = mu
	}
	s.jobMu.Unlock()
	mu.Lock()
	fileUnlock, err := stack.LockJob(id, jobLockWait)
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	return func() {
		fileUnlock()
		mu.Unlock()
	}, nil
}

// checkJobProject refuses a job planned in another project than the one
// this server works in: every step tool (pg_create, k8s_delete, destroy…)
// runs in the server's project, where the job's resources are not.
func (s *Registry) checkJobProject(job *stack.Job) error {
	if job.ProjectID == "" {
		return nil // planned by an older version
	}
	eng, err := s.newEngine(nil)
	if err != nil {
		return err
	}
	if eng == nil || eng.Config.ProjectID == job.ProjectID {
		return nil
	}
	return fmt.Errorf(i18n.T("job %s was planned in project %s, but this server works in project %s: its tools would act on the wrong project. Restart the MCP server with --project-id %s (or CLOUDRU_PROJECT_ID)", "задание %s запланировано в проекте %s, а сервер работает в проекте %s: его инструменты действовали бы не в том проекте. Перезапустите MCP-сервер с --project-id %s (или CLOUDRU_PROJECT_ID)"),
		job.ID, job.ProjectID, eng.Config.ProjectID, job.ProjectID)
}

// jobLockWait bounds the wait for another process's crank of the same job:
// one crank is a few API calls, not minutes.
const jobLockWait = time.Minute

// crankJob loads the job and, while it is in progress, advances and saves
// it, all under the job's lock. Only a "running" job moves: stack_apply with
// confirmed=true sets it, and a read-only registry (SetReadOnly) advances
// none. A "planned" job is shown, never advanced —
// stack_status is a read-only poll, and advancing a planned job would create
// every billable step without the user's approval (QA 15.10.2026, C1).
func (s *Registry) crankJob(ctx context.Context, id string) (*stack.Job, error) {
	unlock, err := s.lockJob(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	job, err := stack.LoadJob(id)
	if err != nil {
		return nil, err
	}
	if job.Status == "running" && !s.readOnly {
		if err := s.checkJobProject(job); err != nil {
			return nil, err
		}
		// The operations of a job of the server's project (checkJobProject)
		// were started by evo's own create calls there, maybe before this
		// server did: a bound registry polls them as its own
		// (kindOperation). A job without a project proves nothing.
		if s.projectBound && job.ProjectID != "" {
			for _, st := range job.Steps {
				if st.OpID != "" && st.PollTool != "" {
					s.ops.remember(groupOf(st.PollTool), st.OpID)
				}
			}
		}
		s.advance(ctx, job)
		if e := job.Save(); e != nil {
			return nil, fmt.Errorf("persist job: %w", e)
		}
	}
	return job, nil
}

func (s *Registry) handleStackStatus(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "job_id")
	if id == "" {
		return usageT("'job_id' is required", "нужен 'job_id'"), true
	}
	// Absorb SOME waiting inside the call (crank every few seconds up to a small
	// budget) so the agent doesn't poll in a tight loop — but keep the budget SHORT.
	// A long internal block is uninterruptible from the gateway's side: an MCP call
	// runs in a separate persistent process, so when the gateway aborts a run
	// (timeout / new input) it just stops awaiting the response — it cannot cancel
	// the in-flight tool. A multi-minute wait then makes the gateway's abort-settle
	// time out → "This operation was aborted" → surface_error → the agent goes
	// silent mid-deploy. Capping at 20s keeps every poll quick and abortable; the
	// agent simply re-polls (cheap: compact O(1) status; empty inter-poll turns are
	// handled by the reasoning-only retry). wait_seconds is clamped, not honored.
	waitBudget := pgInt(args, "wait_seconds", 20)
	if waitBudget > 20 {
		waitBudget = 20
	}
	if waitBudget < 1 {
		waitBudget = 1
	}
	deadline := time.Now().Add(time.Duration(waitBudget) * time.Second)
	for {
		// Reloaded on every crank: another call may have advanced it.
		job, err := s.crankJob(ctx, id)
		if err != nil {
			return errResult(err), true
		}
		if job.Status == "planned" {
			out := job.Compact()
			out["note"] = "Not started: call stack_apply with this job_id and confirmed=true after the user approved the plan. stack_status only reports progress."
			return toJSON(out), false
		}
		if job.Status != "running" {
			return toJSON(job.Compact()), false // terminal (done/error)
		}
		if s.readOnly {
			out := job.Compact()
			out["note"] = "Not advanced: this server runs with --read-only and acts on no consent, the one stack_apply was given included. " +
				"The cloud goes on with the steps already started; a server without --read-only advances the job when it polls it."
			return toJSON(out), false
		}
		rem := time.Until(deadline)
		if rem <= 0 {
			return toJSON(job.Compact()), false // budget exhausted — agent polls again
		}
		select {
		case <-ctx.Done():
			return errResult(ctx.Err()), true
		case <-time.After(min(stackCrankInterval, rem)):
		}
	}
}

// stepMayHaveCreated reports whether a step may have reached the cloud: it
// ran (running, waiting, error after a try) or its create was cut off. Only
// such a step is looked up by name at destroy: a step that never ran owns
// nothing, and a resource with its name belongs to someone else — deleting
// it destroyed a foreign cluster (QA r3 R3).
func stepMayHaveCreated(st *stack.Step) bool {
	if st.Interrupted || st.Attempts > 0 || st.OpID != "" {
		return true
	}
	switch st.Status {
	case "running", "waiting", "done":
		return true
	}
	return false
}

func (s *Registry) handleStackDestroy(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "job_id")
	if id == "" {
		return usageT("'job_id' is required", "нужен 'job_id'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		// The preview names what goes: each step's resource as the job
		// knows it (release audit, R-04, re-run N-11).
		job, err := stack.LoadJob(id)
		if err != nil {
			return errResult(err), true
		}
		// The confirmed call refuses a job of another project: the preview
		// does so before the question (Cursor review of PR #89).
		if err := s.checkJobProject(job); err != nil {
			return errResult(err), true
		}
		var res []map[string]any
		for _, st := range job.Steps {
			rid := firstNonEmptyStr(job.Outputs[st.Key], st.ResourceID)
			if rid == "" && !stepMayHaveCreated(st) {
				continue
			}
			row := map[string]any{"step": st.Key, "status": st.Status}
			if rid != "" {
				row["id"] = rid
			}
			if st.FindName != "" {
				row["name"] = st.FindName
			}
			res = append(res, row)
		}
		out := map[string]any{"status": "confirmation_required",
			"message": i18n.Tf("stack_destroy PERMANENTLY deletes the %d resource(s) of stack %s listed in resources.",
				"stack_destroy БЕЗВОЗВРАТНО удаляет ресурсы стека (%d) %s, перечисленные в resources.", len(res), id),
			"resources": res}
		if len(res) == 0 {
			out["message"] = i18n.Tf("stack %s has created nothing yet; stack_destroy only marks it destroyed.",
				"стек %s ещё ничего не создал; stack_destroy только отметит его удалённым.", id)
		}
		return toJSON(out), true
	}
	unlock, err := s.lockJob(id)
	if err != nil {
		return errResult(err), true
	}
	defer unlock()
	job, err := stack.LoadJob(id)
	if err != nil {
		return errResult(err), true
	}
	// A 404 from another project would read as "already gone" and the job
	// would be marked destroyed with its resources still billed (QA P2).
	if err := s.checkJobProject(job); err != nil {
		return errResult(err), true
	}
	// Reconcile ids from the STEPS before teardown: a resource created but not yet
	// "done" (e.g. a k8s cluster still PROVISIONING) has its id on the step, not in
	// Outputs — without this, destroying mid-deploy would ORPHAN it. Also resolve
	// by-name for async resources whose op hadn't completed.
	for _, st := range job.Steps {
		if job.Outputs[st.Key] != "" {
			continue
		}
		if st.ResourceID != "" {
			job.Outputs[st.Key] = st.ResourceID
		} else if stepMayHaveCreated(st) {
			if id, _ := s.findStepResource(ctx, st); id != "" {
				job.Outputs[st.Key] = id
			}
		}
	}
	// Reverse teardown of what was created. 404/not-found is idempotent success.
	// The code (VM_NOT_FOUND, NOT_FOUND) is checked too: in the CLI the
	// message is in the language of the CLI, and destroy of a VM already
	// gone says "no VM … in project …; nothing was deleted" (live run 18.10:
	// a second stack_destroy turned a destroyed job into destroy_failed).
	// deleting: async deletions the cloud accepted but has not finished
	// (status "deleting" with an operation): Postgres takes ~5 min, so the
	// answer must not read as if it were gone already (release audit, L-37).
	var deleted, deleting, failed []string
	del := func(label, tool string, dargs map[string]any) {
		dargs["confirmed"] = true
		if res, e := s.callTool(ctx, tool, dargs); e != nil {
			msg := e.Error()
			reason, _ := res["reason"].(string)
			if reason == clierrors.CodeVMNotFound || reason == clierrors.CodeNotFound ||
				strings.Contains(msg, "404") || strings.Contains(msg, "NOT_FOUND") ||
				strings.Contains(strings.ToLower(msg), "not found") || strings.Contains(msg, "не найден") {
				deleted = append(deleted, label+" (already gone)")
			} else {
				failed = append(failed, label+": "+msg)
			}
		} else {
			deleted = append(deleted, label)
			if st, _ := res["status"].(string); st == "deleting" {
				deleting = append(deleting, label)
			}
		}
	}
	if id := job.Outputs["dns"]; id != "" {
		del("dns", "dns_record_delete", map[string]any{"record_id": id})
	}
	if id := job.Outputs["lb"]; id != "" {
		del("lb", "lb_delete", map[string]any{"id": id})
	}
	if id := job.Outputs["tg"]; id != "" {
		del("tg", "tg_delete", map[string]any{"id": id})
	}
	if id := job.Outputs["k8s"]; id != "" {
		del("k8s", "k8s_delete", map[string]any{"cluster_id": id})
	}
	if id := job.Outputs["kafka"]; id != "" {
		del("kafka", "kafka_delete", map[string]any{"cluster_id": id})
	}
	if id := job.Outputs["redis"]; id != "" {
		del("redis", "redis_delete", map[string]any{"cluster_id": id})
	}
	if id := job.Outputs["snat"]; id != "" {
		del("snat", "snat_delete", map[string]any{"id": id})
	}
	if id := job.Outputs["registry"]; id != "" {
		del("registry", "ar_delete", map[string]any{"id": id})
	}
	if id := job.Outputs["postgres"]; id != "" {
		del("postgres", "pg_delete", map[string]any{"cluster_id": id})
	}
	if id := job.Outputs["vm"]; id != "" {
		del("vm", "destroy", map[string]any{"vm_id": id})
	}
	for i, sc := range job.Manifest.Secrets {
		if job.Outputs[fmt.Sprintf("secret:%d", i)] != "" {
			del("secret", "secret_delete", map[string]any{"path": sc.Path, "force": true})
		}
	}
	for i, s3 := range job.Manifest.S3 {
		if job.Outputs[fmt.Sprintf("s3:%d", i)] != "" {
			del("s3", "s3_rm", map[string]any{"bucket": s3.Bucket, "key": s3.Key})
		}
	}
	// VPC last — its subnets/resources must be gone first.
	if id := job.Outputs["vpc"]; id != "" {
		del("vpc", "vpc_delete", map[string]any{"id": id})
	}
	// A job with a failed deletion still holds resources: "destroyed" would
	// hide it from the duplicate warning of stack_plan. Calling stack_destroy
	// again retries; what is gone already counts as deleted.
	job.Status = "destroyed"
	if len(failed) > 0 {
		job.Status = "destroy_failed"
	}
	if e := job.Save(); e != nil {
		failed = append(failed, "persist job: "+e.Error())
	}
	out := map[string]any{"status": job.Status, "deleted": deleted, "failed": failed}
	if len(deleting) > 0 {
		out["deleting"] = deleting
		out["note"] = "Deletion accepted; the resources in 'deleting' are still being removed by the cloud (minutes; Postgres ~5 min). Check them with pg_list, k8s_list, lb_list and the like before reusing their names."
	}
	if len(failed) > 0 {
		// Every error answer has a reason (docs/mcp.md «Ошибки»). This one
		// is the reason the CLI has given the answer since it had none
		// (toolError in internal/cli): NOT_FOUND when it says something
		// was not found, else API_ERROR — the same code and exit code
		// (spec.md REQ-8, TestParity_ErrorReasonEqualsTool).
		out["reason"] = clierrors.CodeAPIError
		if low := strings.ToLower(toJSON(out)); strings.Contains(low, "not found") || strings.Contains(low, "не найден") {
			out["reason"] = clierrors.CodeNotFound
		}
	}
	return toJSON(out), len(failed) > 0
}
