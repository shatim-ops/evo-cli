// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import "time"

// DeploymentRecord is the persisted form of a deployment.
type DeploymentRecord struct {
	ID           string `json:"id"`
	VMID         string `json:"vm_id"`
	VMIP         string `json:"vm_ip"`
	FloatingIPID string `json:"floating_ip_id,omitempty"` // released by destroy
	// ProjectID is the Cloud.ru project the VM was created in: destroy and
	// a re-run of deploy work there, whatever project the config now names.
	// Empty in records from before this field existed.
	ProjectID string `json:"project_id,omitempty"`
	// CreatedBy is the key that made the record. Cloud.ru answers 404 for
	// a VM a key does not see, as for one that is gone: under another key a
	// 404 proves nothing, and destroy keeps the record and the floating IP
	// (release plan 1.8, L3; «Риск», conclusion 43). Nil in records from
	// before 1.8.
	CreatedBy *Maker `json:"created_by,omitempty"`
	VMFlavor  string `json:"vm_flavor"`
	Region    string `json:"region"`
	// VMName, ZoneName and FlavorName are what people know the VM, its
	// zone (Region, a UUID) and flavor (VMFlavor, a UUID when sizing
	// picked it) by: destroy and status show them next to the IDs. Empty
	// in records from before v1.0.1; those commands then look them up.
	VMName      string `json:"vm_name,omitempty"`
	ZoneName    string `json:"zone_name,omitempty"`
	FlavorName  string `json:"flavor_name,omitempty"`
	Status      string `json:"status"` // creating, provisioned, deploying, running, error, ssh_timeout, ssh_auth_failed, ssh_host_key_changed, docker_failed, upload_failed, compose_failed, destroying, destroyed
	ComposeHash string `json:"compose_hash"`
	// DeployedHash is the hash of the compose file whose 'docker compose
	// up' finished on the VM, written with "running" after it. ComposeHash
	// is written before SSH, so a deploy cut on the way leaves it ahead of
	// the VM (cloud-e2e 194, 1.8.1). Empty in records from before 1.8.1.
	DeployedHash string `json:"deployed_hash,omitempty"`
	// Source is what made the record: "provision" for a VM of evo vm
	// create, which deploy does not update (D62); "deploy_apply" for a VM
	// of a deploy plan (D82, from 1.8); empty for deploy, and for
	// deploy_apply before 1.8 and every record before 1.6.
	Source string `json:"source,omitempty"`
	// PlanID is the deploy plan a deploy_apply record was made by: the way
	// on of an unfinished one is deploy_apply with it (D82). Empty in
	// records of other sources and from before 1.8.
	PlanID string `json:"plan_id,omitempty"`
	// Cidr is the source deploy opened the record's ports from
	// (sgrules.Source): the repeat of an unfinished deploy opens the
	// missing ones from it, not wider (D82). Empty in records from before
	// 1.8.
	Cidr string `json:"cidr,omitempty"`
	// ComposePath is the absolute path of the compose file the deployment
	// runs: deploy of another one refuses to send it to this VM (D62).
	// Empty in records from before 1.6.
	ComposePath           string `json:"compose_path,omitempty"`
	SSHUser               string `json:"ssh_user,omitempty"`
	SSHKeyFingerprint     string `json:"ssh_key_fingerprint,omitempty"`
	SSHHostKeyFingerprint string `json:"ssh_host_key_fingerprint,omitempty"`
	ExposedPorts          []int  `json:"exposed_ports,omitempty"`
	// SecurityGroupID is the group the VM's interface is in; a re-run of
	// deploy opens missing ports there.
	SecurityGroupID string `json:"security_group_id,omitempty"`
	// SecurityGroupName names that group in messages.
	SecurityGroupName string `json:"security_group_name,omitempty"`
	// SecurityGroupPorts are the ports ("80/tcp", "53/udp") this deployment
	// needs open in that group, whoever created the rules. destroy keeps a
	// rule that another deployment in this state still needs.
	SecurityGroupPorts []string `json:"security_group_ports,omitempty"`
	// OpenedRules are the rules this deployment created (never ones that
	// were there before it); destroy deletes them.
	OpenedRules []SGRule `json:"opened_rules,omitempty"`
	// PortsAlreadyOpen are ports ("80/tcp") a rule this deployment did not
	// create already opened when deploy came to them: deploy created and
	// recorded none, and destroy says it leaves them.
	PortsAlreadyOpen []string `json:"ports_already_open,omitempty"`
	// SSHWait is the wait for SSH a bound of deploy ended (D80): the next
	// deploy goes on with it, so the wait ends after its limit of attempts
	// and says why past its first minutes, as one call of the CLI does.
	// Nil once SSH answered or the wait ended.
	SSHWait   *SSHWait  `json:"ssh_wait,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SSHWait is a wait of deploy for SSH on the VM of a record.
type SSHWait struct {
	// Since is when the wait began.
	Since time.Time `json:"since"`
	// Attempts are the connects that failed so far; Limit, how many the
	// wait makes in all.
	Attempts int `json:"attempts"`
	Limit    int `json:"limit"`
}

// SGRule is a security-group rule created by a deployment.
type SGRule struct {
	ID              string `json:"id"`
	SecurityGroupID string `json:"security_group_id"`
	Port            string `json:"port"` // "80/tcp", "53/udp"
	// DeleteError is the last failed attempt to delete the rule: the record
	// is kept, and the next destroy tries again.
	DeleteError string `json:"delete_error,omitempty"`
}

// File is the root state file structure.
type File struct {
	Version     int                `json:"version"`
	Project     string             `json:"project,omitempty"`
	Deployments []DeploymentRecord `json:"deployments"`
	// LostFloatingIPs are the floating IPs a deploy asked the cloud for
	// with no answer to tell whether it made them: each may exist and be
	// billed. A mark stays until the address is found by its name (taken
	// for its VM, or released by destroy) or destroy names it as not
	// found (D107; «Риск», conclusion 180).
	LostFloatingIPs []LostFloatingIP `json:"lost_floating_ips,omitempty"`
}

// LostFloatingIP is the mark of a floating IP whose create got no answer
// from the cloud: the request may have been accepted. It holds the exact
// name the create gave the address — the one thing the address is found
// by, before it is known to exist.
type LostFloatingIP struct {
	Name      string `json:"name"`
	ProjectID string `json:"project_id,omitempty"`
	// VMName is the VM the address was for; VMID, once that VM exists
	// (an address asked for after it was created).
	VMName      string    `json:"vm_name,omitempty"`
	VMID        string    `json:"vm_id,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
	// Answered tells that a person was asked whether to reserve another
	// address while this one is not found, and said it: the next deploy
	// reserves one without asking again. The mark stays all the same —
	// the first address may still come and be billed.
	Answered  bool   `json:"answered,omitempty"`
	CreatedBy *Maker `json:"created_by,omitempty"`
}

// Maker is the key that made a deployment record: the service account of
// a key evo login made, and the Key ID, masked.
type Maker struct {
	ServiceAccountID string `json:"service_account_id,omitempty"`
	KeyID            string `json:"key_id,omitempty"`
}

// Other tells that cur is surely not the key that made the record: by the
// account when both know it — a new key of the same account (evo login
// replaces it) sees the same — else by the Key ID. Unknown on either side
// is not «other»: the record is read as before 1.8.
func (m *Maker) Other(cur *Maker) bool {
	switch {
	case m == nil || cur == nil:
		return false
	case m.ServiceAccountID != "" && cur.ServiceAccountID != "":
		return m.ServiceAccountID != cur.ServiceAccountID
	case m.KeyID != "" && cur.KeyID != "":
		return m.KeyID != cur.KeyID
	}
	return false
}
