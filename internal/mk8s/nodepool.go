// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// --- Node pool types (v3) ---

type FixedScale struct {
	Count int `json:"count"`
}

type AutoScale struct {
	MinCount     int `json:"minCount"`
	MaxCount     int `json:"maxCount"`
	InitialCount int `json:"initialCount,omitempty"`
}

type ScalePolicy struct {
	FixedScale *FixedScale `json:"fixedScale,omitempty"`
	AutoScale  *AutoScale  `json:"autoScale,omitempty"`
}

type DiskRequest struct {
	TypeName string `json:"typeName,omitempty"`
	Size     int    `json:"size,omitempty"`
}

type MachineConfigurationRequest struct {
	FlavorID string       `json:"flavorId"`
	Disk     *DiskRequest `json:"disk,omitempty"`
}

type NodePoolNetworkConfigurationRequest struct {
	NodesSubnetID   string `json:"nodesSubnetId"`
	SecurityGroupID string `json:"securityGroupId,omitempty"`
}

type NodePoolUpdateConfiguration struct {
	Strategy string `json:"strategy"` // NODE_POOL_UPDATE_STRATEGY_ROLLING_UPDATE
}

// CreateNodePoolReq mirrors the confirmed v3 CreateNodePoolRequest.
type CreateNodePoolReq struct {
	ClusterID            string                              `json:"clusterId"`
	Name                 string                              `json:"name"`
	ScalePolicy          ScalePolicy                         `json:"scalePolicy"`
	MachineConfiguration MachineConfigurationRequest         `json:"machineConfiguration"`
	NetworkConfiguration NodePoolNetworkConfigurationRequest `json:"networkConfiguration"`
	UpdateConfiguration  NodePoolUpdateConfiguration         `json:"updateConfiguration"`
}

// CreateNodePool submits a node-pool create and returns the async operation.
func (c *Client) CreateNodePool(ctx context.Context, req CreateNodePoolReq) (*Operation, error) {
	data, st, err := c.do(ctx, http.MethodPost, "/v3/nodePools", nil, req)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf("parse operation: %w", err)
	}
	return &op, nil
}

// ListNodePools lists the node pools of a cluster, following
// nextPageToken. clusterId is required by the API (openapi-v3.yaml of
// mk8s, NodePoolService_List): without it the call fails, so an empty
// clusterID is refused here.
func (c *Client) ListNodePools(ctx context.Context, clusterID string) ([]map[string]any, error) {
	if clusterID == "" {
		return nil, errors.New("mk8s: node pools need a cluster ID")
	}
	var out []map[string]any
	// Only clusterId, no projectId: the request as the live API answered
	// it (api-probe 17.10). A repeated token and more than maxAddonPages
	// pages are errors (c.walk).
	query := func() url.Values { return url.Values{"clusterId": {clusterID}} }
	err := c.walk(ctx, "/v3/nodePools", query, func(data []byte) (string, error) {
		var r struct {
			NodePools     []map[string]any `json:"nodePools"`
			NextPageToken string           `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", fmt.Errorf("parse node pools: %w", err)
		}
		out = append(out, r.NodePools...)
		return r.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
