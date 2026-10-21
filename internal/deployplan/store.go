// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package deployplan implements the two-phase deploy used by the MCP wizard:
// Resolve() turns a request (auto or manual) into a fully-resolved, costed Plan
// without creating anything, and ApplyWithTimeout() executes a previously-saved Plan.
//
// Plans are persisted to disk so deploy_plan and deploy_apply survive across
// separate MCP tool calls (and even an MCP server restart). Plans never contain
// secrets: passwords are generated at Apply time and returned once.
package deployplan

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// planTTL bounds how long a plan_id stays valid between plan and apply.
// 30 min gives the user room to deliberate / re-confirm without the plan
// expiring mid-conversation.
const planTTL = 30 * time.Minute

// FlavorSpec is the chosen VM size.
type FlavorSpec struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	VCPUs int    `json:"vcpus"`
	RAMGB int    `json:"ram_gb"`
	// FreeTier: the flavor of the free tier — the API's zero price for it
	// is the price, not a failure (QuoteVM).
	FreeTier bool `json:"free_tier,omitempty"`
}

// RefSpec is an id+name reference (image, zone).
type RefSpec struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DiskSpec is the chosen boot disk.
type DiskSpec struct {
	SizeGB int    `json:"size_gb"`
	Type   string `json:"type"`
	TypeID string `json:"type_id,omitempty"`
}

// VMSpec is the resolved VM configuration shown to the user.
type VMSpec struct {
	Name   string     `json:"name"`
	Flavor FlavorSpec `json:"flavor"`
	Image  RefSpec    `json:"image"`
	Zone   RefSpec    `json:"zone"`
	Disk   DiskSpec   `json:"disk"`
}

// NetworkSpec is the resolved network configuration.
type NetworkSpec struct {
	SubnetID        string `json:"subnet_id"`
	SecurityGroupID string `json:"security_group_id"`
	FloatingIP      bool   `json:"floating_ip"`
	AttachFIPID     string `json:"attach_fip_id,omitempty"`
}

// SSHSpec describes how the user will log in.
type SSHSpec struct {
	Login   string `json:"login"`
	KeyName string `json:"key_name"` // "user-key" | "generated-password" | "image-default"
}

// Plan is a fully-resolved, costed deploy plan.
type Plan struct {
	PlanID    string    `json:"plan_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Product   string    `json:"product"`
	Mode      string    `json:"mode"`
	ProjectID string    `json:"project_id"`

	VM              VMSpec           `json:"vm"`
	Network         NetworkSpec      `json:"network"`
	Ports           []int            `json:"ports,omitempty"` // product ports to open in the SG
	SSH             SSHSpec          `json:"ssh"`
	Cost            pricing.Estimate `json:"cost"`
	Warnings        []string         `json:"warnings"`
	ComposeRequired bool             `json:"compose_required"`

	// Internal fields needed by Apply (persisted, no secrets).
	ComposeFile  string            `json:"compose_file,omitempty"`
	MetaFields   []string          `json:"meta_fields,omitempty"`   // image_metadata password fields generated at apply (older plans: also name/hostname/public_key)
	MetaStatic   map[string]string `json:"meta_static,omitempty"`   // image_metadata fields sent verbatim (no generated password; the caller's image_metadata values as given)
	PublicKey    string            `json:"public_key,omitempty"`    // user public key (safe to persist)
	SourceParams Params            `json:"source_params,omitempty"` // the request that produced this plan (for patch/re-plan)
}

// newPlanID returns a short random plan identifier.
func newPlanID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "plan-" + hex.EncodeToString(b)
}

// plansPath is the directory plans are stored in.
func plansPath(stateDir string) string {
	return filepath.Join(config.DataDir(stateDir), "plans")
}

// plansDir returns the directory plans are stored in, creating it.
func plansDir(stateDir string) (string, error) {
	dir := plansPath(stateDir)
	if err := config.MkdirPrivate(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// LockApply takes the lock of applying a plan's VM (project and VM name),
// across processes: two deploy_apply calls at once each saw the VM
// without a floating IP and reserved one, leaving a second billed address
// (break r3a R3A-3). The second call is refused at once instead of
// waiting minutes; called again after the first ends, it adopts the VM.
func LockApply(stateDir string, p *Plan) (unlock func(), err error) {
	dir, err := plansDir(stateDir)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(p.ProjectID + "/" + p.VM.Name))
	unlock, err = state.FileLock(filepath.Join(dir, "apply-"+hex.EncodeToString(sum[:8])+".lock"), 0)
	if errors.Is(err, state.ErrFileLocked) {
		return nil, fmt.Errorf("another deploy_apply for VM %q is running; wait for it to finish, then call deploy_apply again (the VM is adopted, not recreated)", p.VM.Name)
	}
	return unlock, err
}

// Save writes a plan to disk and returns its path.
func Save(stateDir string, p *Plan) error {
	dir, err := plansDir(stateDir)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, p.PlanID+".json"), data, 0o600)
}

// validPlanID reports whether id is what newPlanID makes. The id comes from
// the MCP client and becomes a file name: nothing else may reach the path.
func validPlanID(id string) bool {
	const prefix = "plan-"
	if !strings.HasPrefix(id, prefix) || len(id) != len(prefix)+12 {
		return false
	}
	_, err := hex.DecodeString(id[len(prefix):])
	return err == nil
}

// Load reads a plan by id, enforcing the TTL. It only reads: deploy_apply
// loads the plan for its preview before consent (B12-8), and a read-only
// server reads a base_plan_id, so no directory is made here.
func Load(stateDir, planID string) (*Plan, error) {
	if !validPlanID(planID) {
		return nil, fmt.Errorf(i18n.T("plan %q not found: a plan_id looks like plan-0123456789ab (run deploy_plan to get one)", "план %q не найден: plan_id выглядит как plan-0123456789ab (его выдаёт deploy_plan)"), planID)
	}
	data, err := os.ReadFile(filepath.Join(plansPath(stateDir), planID+".json"))
	if err != nil {
		return nil, fmt.Errorf(i18n.T("plan %q not found (it may have expired; run deploy_plan again)", "план %q не найден (возможно, истёк срок; снова выполните deploy_plan)"), planID)
	}
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("plan %q is corrupt: %w", planID, err)
	}
	if !p.ExpiresAt.IsZero() && time.Now().After(p.ExpiresAt) {
		return nil, fmt.Errorf("plan %q expired at %s; run deploy_plan again", planID, p.ExpiresAt.Format(time.RFC3339))
	}
	return &p, nil
}

// Delete removes a plan file (best-effort; called after a successful apply).
func Delete(stateDir, planID string) {
	if !validPlanID(planID) {
		return
	}
	dir, err := plansDir(stateDir)
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(dir, planID+".json"))
}
