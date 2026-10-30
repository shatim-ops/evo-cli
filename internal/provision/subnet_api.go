// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// CreateSubnetRequest mirrors POST /api/v1/subnets
// (PublicSubnetCreateRequest, additionalProperties: false; the project is
// in the body, the spec has no project_id query here). subnet_address is
// the network with its mask; prefix_length is the size of a network the
// platform picks, and the two together are refused with 422 «Either
// subnet_address or prefix_length should be submitted, not both»
// (cloud-e2e run 150): evo sends the CIDR alone.
type CreateSubnetRequest struct {
	Name               string   `json:"name"`
	ProjectID          string   `json:"project_id"`
	VPCID              string   `json:"vpc_id"`
	SubnetAddress      string   `json:"subnet_address"`
	AvailabilityZoneID string   `json:"availability_zone_id"`
	RoutedNetwork      bool     `json:"routed_network"`
	DNSServers         []string `json:"dns_servers,omitempty"`
	Description        string   `json:"description,omitempty"`
}

// CreateSubnet creates a subnet and returns the raw API object.
func (a *CloudRuAdapter) CreateSubnet(ctx context.Context, req CreateSubnetRequest) (map[string]any, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, "/api/v1/subnets", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// GetSubnet reads a subnet by id; can_delete says whether the platform
// will delete it (false while a VM, a balancer or a cluster is in it).
func (a *CloudRuAdapter) GetSubnet(ctx context.Context, id string) (map[string]any, error) {
	resp, err := a.doWithRetry(ctx, http.MethodGet, fmt.Sprintf("/api/v1/subnets/%s", seg(id)), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// DeleteSubnet deletes a subnet by id. The spec takes no project_id here
// (the subnet's id names it); projectID is the interface's, as the other
// deletes'.
func (a *CloudRuAdapter) DeleteSubnet(ctx context.Context, id, _ string) error {
	resp, err := a.doWithRetry(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/subnets/%s", seg(id)), nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
