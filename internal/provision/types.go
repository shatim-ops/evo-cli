// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// VM represents a Cloud.ru virtual machine.
type VM struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"state"` // running, stopped, error, creating
	FlavorID   string    `json:"flavor_id"`
	ImageID    string    `json:"image_id"`
	Region     string    `json:"availability_zone_id"`
	PublicIP   string    `json:"public_ip,omitempty"`
	FloatingIP string    `json:"floating_ip,omitempty"`
	CreatedAt  time.Time `json:"created_time"`
	// The /api/v1/vms list returns the zone nested, not as a flat id.
	AvailabilityZone *ZoneRef `json:"availability_zone,omitempty"`
}

// UnmarshalJSON also reads the documented nested fields of GET /api/v1/vms
// (openapi-v3): flavor{id}, image{id} and the public address on the
// interfaces — interfaces[].floating_ip.ip_address, or ip_address of a
// direct_ip interface. The flat flavor_id/image_id/public_ip/floating_ip are
// not in the spec and came back empty (BACKLOG [04], api-contract §4.4);
// they are kept for answers that still carry them.
func (v *VM) UnmarshalJSON(b []byte) error {
	type plain VM
	var r struct {
		plain
		Flavor     *ZoneRef      `json:"flavor"`
		Image      *ZoneRef      `json:"image"`
		Interfaces []VMInterface `json:"interfaces"`
		// Shadows plain.CreatedAt: a time without a zone ("…T20:48:44.123")
		// must not make the whole VM — and its state — unreadable.
		CreatedTime string `json:"created_time"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*v = VM(r.plain)
	v.CreatedAt = parseAPITime(r.CreatedTime)
	if v.FlavorID == "" && r.Flavor != nil {
		v.FlavorID = r.Flavor.ID
	}
	if v.ImageID == "" && r.Image != nil {
		v.ImageID = r.Image.ID
	}
	for _, in := range r.Interfaces {
		if v.FloatingIP == "" && in.FloatingIP != nil && in.FloatingIP.IPAddress != "" {
			v.FloatingIP = in.FloatingIP.IPAddress
		}
		if v.PublicIP == "" && in.Type == "direct_ip" && in.IPAddress != "" {
			v.PublicIP = in.IPAddress
		}
	}
	return nil
}

// parseAPITime reads the API's timestamps: RFC 3339 with or without
// fractions, or without a zone (taken as UTC). Anything else is zero.
func parseAPITime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ZoneRef is a nested {id,name} availability-zone reference.
type ZoneRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ZoneID returns the VM's availability-zone id from either the flat or nested field.
func (v *VM) ZoneID() string {
	if v.Region != "" {
		return v.Region
	}
	if v.AvailabilityZone != nil {
		return v.AvailabilityZone.ID
	}
	return ""
}

// CreateVMRequest contains parameters for VM creation (v1.1 schema).
//
// The field set mirrors the request body proven to work against the Cloud.ru
// tenant by the Python reference implementation: it prefers *_name fields
// (flavor_name, image_name, disk_type_name) over UUIDs, carries an explicit
// interfaces section (subnet + security groups + optional attach_external_ip_id
// for "variant A" floating-IP attach at creation), and omits empty public_key
// (an empty string triggers a 500 on this platform).
type CreateVMRequest struct {
	ProjectID          string            `json:"project_id"`
	Name               string            `json:"name"`
	FlavorID           string            `json:"flavor_id,omitempty"`
	FlavorName         string            `json:"flavor_name,omitempty"`
	ImageID            string            `json:"image_id,omitempty"`
	ImageName          string            `json:"image_name,omitempty"`
	AvailabilityZoneID string            `json:"availability_zone_id,omitempty"`
	Disks              []DiskReq         `json:"disks"`
	ImageMetadata      map[string]string `json:"image_metadata,omitempty"`
	Interfaces         []InterfaceReq    `json:"interfaces,omitempty"`
}

// DiskReq describes a disk to attach to a new VM (VmCreateNewDiskItem).
// Either DiskTypeID or DiskTypeName may be set; name is preferred to avoid
// pinning to a UUID that can change between projects/images.
type DiskReq struct {
	Name         string `json:"name"`
	Size         int    `json:"size"`
	DiskTypeID   string `json:"disk_type_id,omitempty"`
	DiskTypeName string `json:"disk_type_name,omitempty"`
}

// InterfaceReq describes a network interface for VM creation (v1.1).
// AttachExternalIPID, when set, attaches an existing available floating IP at
// creation time ("variant A"). new_external_ip is intentionally never sent: it
// triggers 500 ExternalNetNotFound on this platform.
type InterfaceReq struct {
	Type                     string   `json:"type"` // "regular" | "direct_ip"
	SubnetID                 string   `json:"subnet_id,omitempty"`
	InterfaceSecurityEnabled bool     `json:"interface_security_enabled"`
	SecurityGroups           []string `json:"security_groups,omitempty"`
	AttachExternalIPID       string   `json:"attach_external_ip_id,omitempty"`
}

// Subnet represents a Cloud.ru subnet (GET /api/v1/subnets).
type Subnet struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	AvailabilityZoneID string `json:"availability_zone_id"`
	SubnetAddress      string `json:"subnet_address,omitempty"`
	VPCID              string `json:"vpc_id,omitempty"`
	// Default: the zone's main subnet (PublicSubnetListItemResponse.default).
	Default bool `json:"default,omitempty"`
	// RoutedNetwork: a routed subnet; a floating IP binds only to an
	// interface in one (docs, «Публичные IP-адреса», ограничения).
	RoutedNetwork bool `json:"routed_network,omitempty"`
	// DefaultGateway: DHCP answers from it (udp 67), so a closed egress
	// must still allow it.
	DefaultGateway string `json:"default_gateway,omitempty"`
	// DNSServers is nil when the answer has no dns_servers field, empty
	// when the subnet has none: then a VM resolves no names (docs,
	// troubleshooting «Temporary failure in name resolution»).
	DNSServers *[]any `json:"dns_servers,omitempty"`
	State      string `json:"state,omitempty"` // created, error, creating…
	Type       string `json:"type,omitempty"`  // regular, vpc, technical, zone_technical
}

// NoDNS reports a subnet the API lists with an empty dns_servers.
func (s *Subnet) NoDNS() bool {
	return s != nil && s.DNSServers != nil && len(*s.DNSServers) == 0
}

// UnmarshalJSON reads the zone from the documented nested
// availability_zone{id,name}: the flat availability_zone_id is not in the
// spec and was always empty, so subnets were matched to zones by name only
// (api-contract §4.3).
func (s *Subnet) UnmarshalJSON(b []byte) error {
	type plain Subnet
	var r struct {
		plain
		AvailabilityZone *ZoneRef `json:"availability_zone"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*s = Subnet(r.plain)
	if s.AvailabilityZoneID == "" && r.AvailabilityZone != nil {
		s.AvailabilityZoneID = r.AvailabilityZone.ID
	}
	return nil
}

// SecurityGroup represents a Cloud.ru security group (GET /api/v1/security-groups).
// The documented list response has no rules, only their counts: read them
// with ListSecurityGroupRules.
type SecurityGroup struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	AvailabilityZoneID string `json:"availability_zone_id,omitempty"`
	// Default: the zone's default group (API field "default").
	Default bool     `json:"default,omitempty"`
	Rules   []SGRule `json:"rules,omitempty"`
}

// UnmarshalJSON reads the zone from the nested availability_zone{id}, as
// for Subnet.
func (g *SecurityGroup) UnmarshalJSON(b []byte) error {
	type plain SecurityGroup
	var r struct {
		plain
		AvailabilityZone *ZoneRef `json:"availability_zone"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*g = SecurityGroup(r.plain)
	if g.AvailabilityZoneID == "" && r.AvailabilityZone != nil {
		g.AvailabilityZoneID = r.AvailabilityZone.ID
	}
	return nil
}

// SGRule is a security-group rule. Used both for reading existing rules and for
// creating new ones (POST /api/v1/security-groups/{id}/rules). The create
// endpoint requires direction/ether_type/ip_protocol/port_range and rejects
// unknown fields (additionalProperties:false), so id/description/remote/state
// are omitempty.
type SGRule struct {
	ID             string `json:"id,omitempty"`
	Direction      string `json:"direction"`   // ingress | egress
	EtherType      string `json:"ether_type"`  // IPv4 | IPv6
	IPProtocol     string `json:"ip_protocol"` // tcp | udp | icmp
	PortRange      string `json:"port_range"`  // e.g. "80:80"
	RemoteIPPrefix string `json:"remote_ip_prefix,omitempty"`
	Description    string `json:"description,omitempty"`
	State          string `json:"state,omitempty"` // read only: created, creating, deleting, error…
}

// DiskType represents a Cloud.ru disk type (GET /api/v1/disk-types).
type DiskType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AvailabilityZone represents a Cloud.ru availability zone.
type AvailabilityZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Enabled false: the zone takes no new resources (Closed). Nil when
	// the answer has no field.
	Enabled *bool `json:"enabled,omitempty"`
	// Default: the zone the console offers first.
	Default bool `json:"default,omitempty"`
}

// Flavor represents a VM sizing tier.
type Flavor struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	VCPUs    int    `json:"cpu"`
	RAMGB    int    `json:"ram"`
	DiskGB   int    `json:"disk_gb,omitempty"`
	FreeTier bool   `json:"free_tier"`
}

// Image types of the catalogue (GET /api/v1/images?type=…).
const (
	ImageTypePublic      = "public"
	ImageTypeMarketplace = "marketplace"
	ImageTypePrivate     = "private"
)

// Image represents a Cloud.ru VM boot image (PublicImageResponse of the
// compute spec, without the icon, the cloud-init template and the fields:
// those come from GetImage).
type Image struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	// Type is public, marketplace or private (an image of a project).
	Type      string `json:"type,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Public    bool   `json:"public,omitempty"`
	// MinCPU is in vCPU, MinRAM and MinDisk in GB.
	MinCPU      int    `json:"min_cpu,omitempty"`
	MinRAM      int    `json:"min_ram,omitempty"`
	MinDisk     int    `json:"min_disk,omitempty"`
	CreatedTime string `json:"created_time,omitempty"`
	OS          string `json:"os"`     // not in the spec; usually empty
	Status      string `json:"status"` // not in the spec; usually empty
	// AvailZones is the documented build state per zone
	// (availability_zones[].state, .enabled; api-contract §4.6).
	AvailZones []ImageAvailZone `json:"availability_zones,omitempty"`
	// NotInProject marks a shared image that the project's own view of the
	// catalogue (GET /images?project_id=) leaves out: creating a VM from it
	// fails with 422 wrong_image (seen live 16.10.2026). images.Catalog sets
	// it; the API never sends it.
	NotInProject bool `json:"-"`
}

// ImageFilter is the server-side query of GET /api/v1/images. Name and
// DisplayName match a substring, case-insensitive (checked live 16.10.2026).
// Zero fields are not sent.
type ImageFilter struct {
	Name        string
	DisplayName string
	// Type is public, marketplace or private.
	Type               string
	Enabled            *bool
	NeedGPU            *bool
	AvailabilityZoneID string
	// ProjectID asks for the catalogue as this project sees it: the shared
	// images the project can use plus its own. It combines with the other
	// fields (checked live 16.10.2026).
	ProjectID string
}

// Query is the filter as URL parameters.
func (f ImageFilter) Query() url.Values {
	q := url.Values{}
	set := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	set("name", f.Name)
	set("display_name", f.DisplayName)
	set("type", f.Type)
	set("availability_zone_id", f.AvailabilityZoneID)
	set("project_id", f.ProjectID)
	if f.Enabled != nil {
		q.Set("enabled", strconv.FormatBool(*f.Enabled))
	}
	if f.NeedGPU != nil {
		q.Set("need_gpu", strconv.FormatBool(*f.NeedGPU))
	}
	return q
}

// ZoneReady reports whether a VM can be created from the image in this
// zone: the zone is enabled (or says nothing) and the build is "created".
func ZoneReady(az ImageAvailZone) bool {
	return az.State == "created" && (az.Enabled == nil || *az.Enabled)
}

// ReadyZones are the zones the image can boot a VM in, in answer order.
func (i *Image) ReadyZones() []ImageAvailZone {
	var out []ImageAvailZone
	for _, az := range i.AvailZones {
		if ZoneReady(az) {
			out = append(out, az)
		}
	}
	return out
}

// Usable reports whether a VM can be created from the image: built and
// enabled in at least one zone, by the per-zone state when the answer has
// it, else by the legacy top-level status.
func (i *Image) Usable() bool {
	if len(i.AvailZones) > 0 {
		return len(i.ReadyZones()) > 0
	}
	switch strings.ToLower(i.Status) {
	case "deleted", "error", "creating":
		return false
	}
	return true
}

// VMFailed reports whether a VM status is a failure: "error", or the
// "error_<step>" the API gives when a step fails (ru.AZ-1 answered
// "error_creating" after 19 min in creating, cloud-e2e 15.10.2026).
func VMFailed(status string) bool {
	s := strings.ToLower(status)
	return s == "error" || strings.HasPrefix(s, "error_")
}

// ImageDetail contains full image info including required metadata fields.
type ImageDetail struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Type        string `json:"type,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	Public      bool   `json:"public,omitempty"`
	CreatedTime string `json:"created_time,omitempty"`
	OS          string `json:"os"`
	Status      string `json:"status"`
	MinCPU      int    `json:"min_cpu"`
	MinRAM      int    `json:"min_ram"`
	MinDisk     int    `json:"min_disk"`
	// Icon is the catalogue's SVG: kilobytes nobody reads in a terminal or
	// an agent's context; image_get gives it only with full=true.
	Icon string `json:"icon,omitempty"`
	// UserDataTemplate is the cloud-init template the platform renders when a
	// VM is created from this image; MetadataFields declares the values it
	// substitutes.
	UserDataTemplate string               `json:"user_data_template"`
	MetadataFields   []ImageMetadataField `json:"image_metadata"`
	// AvailZones carries the real build state: the top-level Status is often
	// empty, and an image is usable only once a zone reports "created".
	AvailZones []ImageAvailZone `json:"availability_zones"`
	// NotInProject is Image.NotInProject for an image read by UUID.
	NotInProject bool `json:"-"`
}

// ImageAvailZone is an image's per-zone build state and size.
type ImageAvailZone struct {
	AvailabilityZoneID   string `json:"availability_zone_id"`
	AvailabilityZoneName string `json:"availability_zone_name"`
	State                string `json:"state"`
	Size                 int64  `json:"size"`
	Uploaded             bool   `json:"uploaded"`
	Default              bool   `json:"default"`
	// Enabled is nil when the answer has no such field: then the image is
	// not dropped for it.
	Enabled *bool `json:"enabled,omitempty"`
}

// Ready reports whether the image finished building in at least one zone.
func (i *ImageDetail) Ready() bool {
	for _, az := range i.AvailZones {
		if az.State == "created" {
			return true
		}
	}
	return false
}

// ImageMetadataField describes a metadata field declared by an image. A
// field is required through its mandatory group: at least one field of each
// group must be filled. The catalogue's groups are dirty (a leading space,
// Russian, null), so compare them with images.NormGroup.
type ImageMetadataField struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	DisplayPosition int    `json:"display_position"`
	DisplayGroup    string `json:"display_group"`
	MandatoryGroup  string `json:"mandatory_group"`
	// MandatoryGroupDisplayName is the group's label in the console form.
	MandatoryGroupDisplayName string `json:"mandatory_group_display_name"`
	DefaultValue              string `json:"default_value"`
	FieldType                 string `json:"field_type"`
	Sensitive                 bool   `json:"sensitive"`
}

// FloatingIP represents a Cloud.ru floating (public) IP address.
// Interface/VM are raw because the list endpoint may return them as nested
// objects, strings, or omit them; presence is what signals "in use".
type FloatingIP struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	IPAddress string          `json:"ip_address"`
	State     string          `json:"state"`
	Interface json.RawMessage `json:"interface,omitempty"`
	VM        json.RawMessage `json:"vm,omitempty"`
	// NATGateway: the SNAT gateway the address belongs to
	// (PublicFloatingIpResponse.nat_gateway). Such an address is the
	// gateway's: it cannot be released on its own (DELETE answers 422
	// floating_ip_address_is_used_by_nat_gateway; docs, «Публичные
	// IP-адреса», FAQ).
	NATGateway json.RawMessage `json:"nat_gateway,omitempty"`
}

// BoundTo reports whether the answer names VM vmID as the address's: in
// vm, or in the interface it is attached to. The shape of both is not
// documented, so the ID is looked for as a JSON string in either.
func (f FloatingIP) BoundTo(vmID string) bool {
	if vmID == "" {
		return false
	}
	q := []byte(strconv.Quote(vmID))
	return bytes.Contains(f.VM, q) || bytes.Contains(f.Interface, q)
}

// OnNATGateway reports an address bound to a SNAT gateway.
func (f FloatingIP) OnNATGateway() bool { return !isEmptyRaw(f.NATGateway) }

// Available reports whether this floating IP can be attached to a new VM.
// Mirrors the Python reference: state=="available", or no interface/vm bound
// (some tenants omit the state field on the list endpoint). An address of a
// SNAT gateway is in use whatever its state. Any other state is not free
// either (FloatingIpState: in_use, creating, attaching, detaching, updating,
// deleting, error*): an address stuck in error_deleting has no binding but
// can be neither attached nor deleted (cloud-e2e 17.10.2026, register NET-7).
func (f FloatingIP) Available() bool {
	if f.OnNATGateway() {
		return false
	}
	switch f.State {
	case "available":
		return true
	case "":
		return isEmptyRaw(f.Interface) && isEmptyRaw(f.VM)
	}
	return false
}

func isEmptyRaw(r json.RawMessage) bool {
	s := string(r)
	return s == "" || s == "null" || s == "\"\"" || s == "{}" || s == "[]"
}

// CreateFloatingIPRequest contains parameters for floating IP creation.
type CreateFloatingIPRequest struct {
	Name               string `json:"name"`
	ProjectID          string `json:"project_id"`
	InterfaceID        string `json:"interface_id,omitempty"` // empty: a free FIP, attached later
	AvailabilityZoneID string `json:"availability_zone_id"`
}

// VMDetail is the full VM representation from the API including interfaces.
type VMDetail struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	State      string        `json:"state"`
	Interfaces []VMInterface `json:"interfaces"`
}

// AttachedFloatingIP is the address and ID of the first floating IP bound to
// one of the VM's interfaces, or empty strings.
func (d *VMDetail) AttachedFloatingIP() (ip, id string) {
	for _, in := range d.Interfaces {
		if in.FloatingIP != nil && in.FloatingIP.IPAddress != "" {
			return in.FloatingIP.IPAddress, in.FloatingIP.ID
		}
	}
	return "", ""
}

// VMInterface represents a network interface attached to a VM.
//
// The API's ip_address carries the PRIVATE (subnet-internal) address when
// Type=="regular", and a directly-attached PUBLIC address when
// Type=="direct_ip". A regular interface may additionally have a FloatingIP
// (the public address bound to it). Callers that need a VM's private vs public
// IP must branch on Type — see handleVMGet.
type VMInterface struct {
	ID         string           `json:"id"`
	Name       string           `json:"name,omitempty"`
	IPAddress  string           `json:"ip_address"`
	Type       string           `json:"type,omitempty"` // "regular" | "direct_ip"
	Primary    bool             `json:"primary"`
	State      string           `json:"state"`
	FloatingIP *IfaceFloatingIP `json:"floating_ip,omitempty"`
}

// IfaceFloatingIP is the public (floating) IP bound to a VM interface.
type IfaceFloatingIP struct {
	ID        string `json:"id"`
	IPAddress string `json:"ip_address"`
	State     string `json:"state"`
}

// paginatedResponse wraps paginated API responses.
type paginatedResponse[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}
