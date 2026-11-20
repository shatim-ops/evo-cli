// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"net/http"
	"net/url"
	"strings"
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

// NATDeleteInventory requests a complete, project-consistent list for a
// destructive decision. Ordinary discovery retains its existing decoding.
func NATDeleteInventory(ctx context.Context, cloud CloudAPI, project string) ([]map[string]any, error) {
	rows, err := cloud.ListNATGateways(context.WithValue(ctx, natDeleteInventoryKey{}, project), project)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := validateNATOwner(row, project); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

type natDeleteInventoryKey struct{}

func validateNATPage(body []byte, project string, seen map[string]bool, promised int) error {
	var envelope paginatedResponse[map[string]any]
	var rows []map[string]any
	if json.Unmarshal(body, &envelope) == nil {
		rows = envelope.Items
	}
	if rows == nil {
		if err := json.Unmarshal(body, &rows); err != nil || rows == nil {
			return clierrors.New(clierrors.CodeAPIError, i18n.T("Expected a complete gateway list.", "Ожидался полный список шлюзов."), hintUnexpectedFormat())
		}
		// A bare array ends the walk. Count overlap with all preceding pages.
		if promised > 0 {
			fresh := map[string]bool{}
			for _, row := range rows {
				id, _ := row["id"].(string)
				if id != "" && !seen[id] {
					fresh[id] = true
				}
			}
			if len(seen)+len(fresh) < promised {
				return incompleteNATInventory(len(seen)+len(fresh), promised)
			}
		}
	}
	for _, row := range rows {
		if err := validateNATOwner(row, project); err != nil {
			return err
		}
	}
	return nil
}

func incompleteNATInventory(read, total int) error {
	return clierrors.New(clierrors.CodeAPIError, i18n.Tf("The gateway list ended after %d of %d distinct IDs.", "Список шлюзов закончился после %d из %d уникальных ID.", read, total), "")
}

func validateNATOwner(row map[string]any, project string) error {
	id, _ := row["id"].(string)
	if strings.TrimSpace(id) == "" {
		return clierrors.New(clierrors.CodeAPIError, i18n.T("The gateway list contains a record without an ID.", "В списке шлюзов есть запись без ID."), "")
	}
	owners := []any{row["project_id"]}
	if value := row["project"]; value != nil {
		owner, ok := value.(map[string]any)
		if !ok {
			return invalidNATOwner()
		}
		owners = append(owners, owner["id"])
	}
	for _, owner := range owners {
		if owner != nil && owner != "" && owner != project {
			return invalidNATOwner()
		}
	}
	return nil
}

func invalidNATOwner() error {
	return clierrors.New(clierrors.CodeAPIError, i18n.T("The gateway list contradicts the selected project.", "Список шлюзов противоречит выбранному проекту."), "")
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
	policy := computeRetry
	policy.DropMethods = nil
	resp, err := a.doWithPolicy(ctx, http.MethodDelete, path, nil, policy)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
