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
	VMFlavor  string `json:"vm_flavor"`
	Region    string `json:"region"`
	// VMName, ZoneName and FlavorName are what people know the VM, its
	// zone (Region, a UUID) and flavor (VMFlavor, a UUID when sizing
	// picked it) by: destroy and status show them next to the IDs. Empty
	// in records from before v1.0.1; those commands then look them up.
	VMName                string `json:"vm_name,omitempty"`
	ZoneName              string `json:"zone_name,omitempty"`
	FlavorName            string `json:"flavor_name,omitempty"`
	Status                string `json:"status"` // creating, provisioned, deploying, running, error, ssh_timeout, ssh_auth_failed, ssh_host_key_changed, docker_failed, destroying, destroyed
	ComposeHash           string `json:"compose_hash"`
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
	PortsAlreadyOpen []string  `json:"ports_already_open,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
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
}
