// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// NATTypePublicSNAT is the only nat_type the Cloud.ru Compute API accepts for a
// SNAT gateway (confirmed via the live create-schema validator — the enum has
// exactly this one value, spelled with the lowercase-s "sNAT").
const NATTypePublicSNAT = "Public sNAT"

// CreateNATGatewayRequest mirrors POST /api/v1/nat-gateways.
//
// Required fields (confirmed against the live 422 validator): name, nat_type,
// project_id, and exactly one of availability_zone_id / availability_zone_name
// (setting both, or neither, is rejected). The endpoint enforces
// additionalProperties:false, so no extra fields may be sent. A SNAT gateway is
// a zone-level resource — no subnet is passed at create time; it provides
// outbound egress for private subnets in that availability zone.
type CreateNATGatewayRequest struct {
	Name                 string `json:"name"`
	NATType              string `json:"nat_type"`
	ProjectID            string `json:"project_id"`
	AvailabilityZoneID   string `json:"availability_zone_id,omitempty"`
	AvailabilityZoneName string `json:"availability_zone_name,omitempty"`
}

// ListNATGateways lists SNAT gateways in a project.
func (a *CloudRuAdapter) ListNATGateways(ctx context.Context, projectID string) ([]map[string]any, error) {
	// Every page, as ListDisks.
	return listRaw(a, ctx, "/api/v1/nat-gateways", projectID)
}

// CreateNATGateway creates a SNAT gateway and returns the raw API object (its
// shape is passed through so callers can surface id/status without this client
// having to track every field).
func (a *CloudRuAdapter) CreateNATGateway(ctx context.Context, req CreateNATGatewayRequest) (map[string]any, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/nat-gateways"
	if req.ProjectID != "" {
		path += "?project_id=" + url.QueryEscape(req.ProjectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, path, body)
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

// DeleteNATGateway deletes a SNAT gateway by id (project-scoped, mirroring the
// other /api/v1 delete endpoints).
func (a *CloudRuAdapter) DeleteNATGateway(ctx context.Context, id, projectID string) error {
	path := fmt.Sprintf("/api/v1/nat-gateways/%s", seg(id))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
