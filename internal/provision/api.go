// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import "context"

// CloudAPI abstracts Cloud.ru Compute API operations.
type CloudAPI interface {
	SetToken(token string)
	CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
	GetVM(ctx context.Context, id string) (*VM, error)
	DeleteVM(ctx context.Context, id string) error
	ListFlavors(ctx context.Context) ([]*Flavor, error)
	ListAvailabilityZones(ctx context.Context) ([]*AvailabilityZone, error)
	// ListImages lists the shared catalogue; an optional filter becomes the
	// server-side query.
	ListImages(ctx context.Context, filter ...ImageFilter) ([]*Image, error)
	ListImagesInProject(ctx context.Context, projectID string) ([]*Image, error)
	GetImage(ctx context.Context, id string) (*ImageDetail, error)
	// CreateImage builds an image from a bootable disk; DeleteImage removes one.
	CreateImage(ctx context.Context, req CreateImageReq) (map[string]any, error)
	DeleteImage(ctx context.Context, imageID, projectID string) error
	GetVMDetail(ctx context.Context, id string) (*VMDetail, error)
	CreateFloatingIP(ctx context.Context, req CreateFloatingIPRequest) (*FloatingIP, error)
	GetFloatingIP(ctx context.Context, id string) (*FloatingIP, error)

	// Project-scoped discovery (Cloud.ru requires project_id on these endpoints).
	ListDiskTypes(ctx context.Context, projectID string) ([]*DiskType, error)
	ListSubnets(ctx context.Context, projectID string) ([]*Subnet, error)
	// Subnets — create, read one (can_delete) and delete.
	CreateSubnet(ctx context.Context, req CreateSubnetRequest) (map[string]any, error)
	GetSubnet(ctx context.Context, id string) (map[string]any, error)
	DeleteSubnet(ctx context.Context, id, projectID string) error
	ListSecurityGroups(ctx context.Context, projectID string) ([]*SecurityGroup, error)
	ListFloatingIPs(ctx context.Context, projectID string) ([]*FloatingIP, error)
	// AttachFloatingIP binds an existing floating IP to a VM interface (PUT).
	AttachFloatingIP(ctx context.Context, fipID, projectID, interfaceID, name string) (*FloatingIP, error)
	// DeleteFloatingIP releases (deletes) a floating IP by ID.
	DeleteFloatingIP(ctx context.Context, fipID, projectID string) error

	// Project-scoped VM ops (the /vms endpoint requires project_id here).
	ListVMsInProject(ctx context.Context, projectID string) ([]*VM, error)
	DeleteVMInProject(ctx context.Context, id, projectID string) error

	// AddSecurityGroupRule adds one ingress/egress rule to a security group
	// and returns the created rule (its ID, when the API sent one).
	AddSecurityGroupRule(ctx context.Context, sgID, projectID string, rule SGRule) (*SGRule, error)
	// ListSecurityGroupRules lists a group's rules (the group list has none).
	ListSecurityGroupRules(ctx context.Context, sgID string) ([]*SGRule, error)
	// DeleteSecurityGroupRule deletes one rule of a group by ID.
	DeleteSecurityGroupRule(ctx context.Context, sgID, ruleID string) error

	// NAT (SNAT) gateways — outbound internet egress for private subnets
	// (e.g. mk8s worker nodes). Zone-level resource on the Compute API.
	ListNATGateways(ctx context.Context, projectID string) ([]map[string]any, error)
	CreateNATGateway(ctx context.Context, req CreateNATGatewayRequest) (map[string]any, error)
	DeleteNATGateway(ctx context.Context, id, projectID string) error

	// Disks (block storage) — create/attach/detach/delete + list and get.
	ListDisks(ctx context.Context, projectID string) ([]map[string]any, error)
	GetDisk(ctx context.Context, diskID string) (map[string]any, error)
	CreateDisk(ctx context.Context, req CreateDiskReq) (map[string]any, error)
	AttachDisk(ctx context.Context, diskID, vmID, projectID string) (map[string]any, error)
	DetachDisk(ctx context.Context, diskID, vmID, projectID string) (map[string]any, error)
	ResizeDisk(ctx context.Context, diskID string, sizeGB int, projectID string) (map[string]any, error)
	DeleteDisk(ctx context.Context, diskID, projectID string) error

	// PriceCalculation computes the price of resources (POST, no side
	// effects: nothing is created or reserved).
	PriceCalculation(ctx context.Context, projectID string, req PriceCalculationRequest) (*PriceCalculation, error)
}
