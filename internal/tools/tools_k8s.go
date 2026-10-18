// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// k8sClient builds an authenticated mk8s client and returns it together with
// the service-account id (from the IAM token's `sub` claim) used as clusterSaId.
func (s *Registry) k8sClient(ctx context.Context) (*mk8s.Client, string, string, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, "", "", err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, "", "", err
	}
	pid := eng.Config.ProjectID
	cli := mk8s.New(tok.Value, pid, "")
	return cli, pid, jwtSub(tok.Value), nil
}

// jwtSub extracts the `sub` claim (service-account id) from a JWT.
func jwtSub(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	seg := parts[1]
	if m := len(seg) % 4; m != 0 {
		seg += strings.Repeat("=", 4-m)
	}
	b, err := base64.URLEncoding.DecodeString(seg)
	if err != nil {
		return ""
	}
	var c struct {
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(b, &c)
	return c.Sub
}

// handleK8sList lists Managed Kubernetes clusters.
func (s *Registry) handleK8sList(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	cs, err := cli.ListClusters(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{"id": c.ID, "name": c.Name, "status": c.Status})
	}
	return toJSON(map[string]any{"clusters": out}), false
}

// handleK8sGet returns a cluster by id.
func (s *Registry) handleK8sGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageResult(fmt.Errorf("cluster_id is required")), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// handleK8sOptions returns available k8s versions, flavors and zones.
func (s *Registry) handleK8sOptions(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	pc, err := cli.ProductConfiguration(ctx)
	if err != nil {
		return errResult(err), true
	}
	versions := map[string][]string{}
	for _, ch := range pc.Channels {
		versions[ch.Channel] = ch.AvailableVersions
	}
	flavors := make([]map[string]any, 0, len(pc.Flavors))
	for _, f := range pc.Flavors {
		if f.GPU == 0 {
			flavors = append(flavors, map[string]any{"flavor_id": f.FlavorID, "name": f.Name, "cpu": f.CPU, "ram": f.RAM})
		}
	}
	zones := make([]string, 0, len(pc.Zones))
	for _, z := range pc.Zones {
		zones = append(zones, z.Name)
	}
	return toJSON(map[string]any{
		"versions_by_channel": versions,
		"flavors":             flavors,
		"zones":               zones,
		"defaults":            map[string]any{"zone": pc.DefaultZone, "channel": pc.DefaultChannel, "disk_type": pc.DefaultDiskType},
	}), false
}

// resolveK8sPlan resolves the create parameters (version, flavor, zone,
// subnet, SA) and returns them with the zone name.
func (s *Registry) resolveK8sPlan(ctx context.Context, args map[string]any) (mk8s.CreateClusterReq, string, []string, error) {
	var warnings []string
	cli, pid, saID, err := s.k8sClient(ctx)
	if err != nil {
		return mk8s.CreateClusterReq{}, "", nil, err
	}
	pc, err := cli.ProductConfiguration(ctx)
	if err != nil {
		return mk8s.CreateClusterReq{}, "", nil, err
	}

	// Version: explicit, else newest STABLE, else first available.
	version := stringArg(args, "version")
	if version == "" {
		for _, ch := range pc.Channels {
			if strings.Contains(ch.Channel, "STABLE") && len(ch.AvailableVersions) > 0 {
				version = ch.AvailableVersions[0]
				break
			}
		}
		if version == "" {
			for _, ch := range pc.Channels {
				if len(ch.AvailableVersions) > 0 {
					version = ch.AvailableVersions[0]
					break
				}
			}
		}
	}

	// Flavor: explicit name, else smallest non-GPU that fits cpu/ram, else gen-2-4.
	cpu := intArg(args, "cpu", 2)
	ram := intArg(args, "ram_gb", 4)
	flavorName := stringArg(args, "flavor")
	flavorID := ""
	var bestName string
	var bestCPU, bestRAM int
	for _, f := range pc.Flavors {
		if f.GPU != 0 {
			continue
		}
		if flavorName != "" {
			if f.Name == flavorName {
				flavorID = f.FlavorID
				break
			}
			continue
		}
		if f.CPU >= cpu && f.RAM >= ram {
			if flavorID == "" || f.CPU < bestCPU || (f.CPU == bestCPU && f.RAM < bestRAM) {
				flavorID, bestName, bestCPU, bestRAM = f.FlavorID, f.Name, f.CPU, f.RAM
			}
		}
	}
	if flavorID == "" {
		return mk8s.CreateClusterReq{}, "", nil, fmt.Errorf("no matching k8s flavor (cpu>=%d ram>=%d); use k8s_options to see flavors", cpu, ram)
	}
	_ = bestName

	// Master count: 1, 3 or 5 (checked by k8sArgsError before the gate).
	masterCount := intArg(args, "master_count", 1)

	// Zone: the argument, else the zone of subnet_id, else the default of
	// productConfiguration.
	zoneArg := stringArg(args, "zone")
	if zoneArg == "" && stringArg(args, "subnet_id") != "" {
		zoneArg = s.subnetZoneID(ctx, pid, stringArg(args, "subnet_id"))
	}
	zone, err := s.k8sZone(ctx, pc, zoneArg)
	if err != nil {
		return mk8s.CreateClusterReq{}, "", nil, err
	}
	subnetID, err := s.k8sVIPSubnet(ctx, pid, stringArg(args, "subnet_id"), zone)
	if err != nil {
		return mk8s.CreateClusterReq{}, "", nil, err
	}

	// Cluster service account: explicit, else the agent's own (token sub).
	clusterSaID := firstNonEmptyStr(stringArg(args, "cluster_sa_id"), saID)
	if clusterSaID == "" {
		return mk8s.CreateClusterReq{}, "", nil, fmt.Errorf("could not resolve cluster service account id; pass cluster_sa_id")
	}

	name := stringArg(args, "name")
	if name == "" {
		name = "k8s-" + firstNonEmptyStr(stringArg(args, "product"), "cluster")
	}

	kubeApiInternet := true
	if v, ok := args["kube_api_internet"].(bool); ok {
		kubeApiInternet = v
	}

	req := mk8s.CreateClusterReq{
		Name:      name,
		ProjectID: pid,
		ControlPlaneConfiguration: mk8s.ControlPlaneConfiguration{
			Version:             version,
			SizingConfiguration: mk8s.SizingConfiguration{MasterCount: masterCount, FlavorID: flavorID},
			// Without zones the API puts the masters in its default zone
			// (ru.AZ-2), away from the VIP subnet of another zone: the
			// cluster's nodes stay NotReady (release audit, L-08).
			Zones: []string{zone.mk8sID},
		},
		NetworkConfiguration:  mk8s.NetworkConfiguration{PrivateVipSubnetID: subnetID, KubeApiInternet: kubeApiInternet},
		IdentityConfiguration: mk8s.IdentityConfiguration{ClusterSaID: clusterSaID},
	}
	return req, zone.name, warnings, nil
}

// handleK8sPlan resolves and shows the cluster config without creating it.
func (s *Registry) handleK8sPlan(ctx context.Context, args map[string]any) (string, bool) {
	if err := k8sArgsError(args); err != nil {
		return errResult(err), true
	}
	req, zoneName, warnings, err := s.resolveK8sPlan(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"name":              req.Name,
		"zone":              zoneName,
		"version":           req.ControlPlaneConfiguration.Version,
		"master_count":      req.ControlPlaneConfiguration.SizingConfiguration.MasterCount,
		"flavor_id":         req.ControlPlaneConfiguration.SizingConfiguration.FlavorID,
		"subnet_id":         req.NetworkConfiguration.PrivateVipSubnetID,
		"cluster_sa_id":     req.IdentityConfiguration.ClusterSaID,
		"kube_api_internet": req.NetworkConfiguration.KubeApiInternet,
		"warnings":          warnings,
		"note":              "Read-only plan. On the user's approval call k8s_create with the same args + confirmed=true. Cluster creation is async (5-10 min per the docs, 7-12 seen live); poll k8s_get. Add workers with k8s_add_nodepool.",
	}), false
}

// handleK8sCreate creates a cluster (gated on confirmation). Async.
func (s *Registry) handleK8sCreate(ctx context.Context, args map[string]any) (string, bool) {
	if err := k8sArgsError(args); err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "k8s_create builds a real (billable) Kubernetes cluster. Show the k8s_plan to the user; call again with confirmed=true after approval.",
			"preview": k8sPreview(engine.ResourceK8sCluster,
				firstNonEmptyStr(stringArg(args, "name"), "k8s-"+firstNonEmptyStr(stringArg(args, "product"), "cluster")), args),
		}), true
	}
	req, _, _, err := s.resolveK8sPlan(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.CreateCluster(ctx, req)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"status":       "creating",
		"cluster_id":   op.ResourceID,
		"operation_id": op.ID,
		"name":         req.Name,
		"note":         "Cluster is provisioning (async, about 10 min). Poll k8s_get with this cluster_id until status is running, then k8s_kubeconfig.",
	}), false
}

// handleK8sKubeconfig returns the kubeconfig for a cluster.
func (s *Registry) handleK8sKubeconfig(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageResult(fmt.Errorf("cluster_id is required")), true
	}
	network := stringArg(args, "network") // NETWORK_TYPE_PUBLIC | NETWORK_TYPE_PRIVATE
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	cfg, err := cli.GetKubeConfig(ctx, id, network)
	if err != nil {
		return errResult(err), true
	}
	// The API returns base64; decode for convenience.
	plain := cfg
	if b, e := base64.StdEncoding.DecodeString(cfg); e == nil && len(b) > 0 {
		plain = string(b)
	}

	// By default embed a static bearer token so the kubeconfig works with a
	// plain kubectl/helm. The raw Cloud.ru kubeconfig authenticates via a
	// `cloudlogin` exec-plugin that is NOT installed by default → kubectl fails
	// "executable cloudlogin not found". embed_token=false returns the raw one.
	embed := true
	if v, ok := args["embed_token"].(bool); ok {
		embed = v
	}
	if !embed {
		return toJSON(map[string]any{"kubeconfig": plain, "auth": "cloudlogin exec-plugin (must be installed)"}), false
	}
	keyID, secret := s.accessKey()
	if keyID == "" || secret == "" {
		return toJSON(map[string]any{
			"kubeconfig": plain,
			"warning":    "embed_token requested but no Cloud.ru access key is configured (evo configure, or CLOUDRU_KEY_ID/CLOUDRU_SECRET) — returned the raw kubeconfig, which needs the 'cloudlogin' exec plugin.",
		}), false
	}
	tok, e := mk8s.FetchOIDCToken(ctx, mk8s.OIDCURLFromKubeconfig(plain), keyID, secret)
	if e != nil {
		return errResult(fmt.Errorf("embed_token: OIDC token exchange failed: %w", e)), true
	}
	embedded, e := mk8s.EmbedToken(plain, tok)
	if e != nil {
		return errResult(fmt.Errorf("embed_token: %w", e)), true
	}
	return toJSON(map[string]any{
		"kubeconfig": embedded,
		"auth":       "embedded bearer token (no cloudlogin needed)",
		"note":       "Token TTL ~1h — re-fetch the kubeconfig if kubectl starts returning 401.",
	}), false
}

// handleK8sDelete deletes a cluster (gated on confirmation). Async.
func (s *Registry) handleK8sDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageResult(fmt.Errorf("cluster_id is required")), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return deletePreview("k8s_delete permanently deletes cluster %s. Call again with confirmed=true after the user approves.",
			id, func() (map[string]any, error) { return cli.GetCluster(ctx, id) })
	}
	op, err := cli.DeleteCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "deleting", "cluster_id": id, "operation_id": op.ID}), false
}

// handleK8sNodePools lists node pools of a cluster. The API has no list
// across clusters: GET /v3/nodePools requires clusterId (the tool used to
// call it without one and got API_ERROR, cloud-e2e readonly #23).
func (s *Registry) handleK8sNodePools(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	if clusterID == "" {
		return errResult(clierrors.New(clierrors.CodeUsage, "cluster_id is required: the API lists node pools of one cluster",
			"Find the cluster ID with k8s_list (evo k8s list).")), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	nps, err := cli.ListNodePools(ctx, clusterID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(nps))
	for _, np := range nps {
		row := map[string]any{
			"id":      np["id"],
			"name":    np["name"],
			"status":  np["status"],
			"version": np["version"],
		}
		// Desired node count comes from the scale policy (fixed or auto).
		if sp, ok := np["scalePolicy"].(map[string]any); ok {
			if fs, ok := sp["fixedScale"].(map[string]any); ok {
				row["desired"] = fs["count"]
			} else if as, ok := sp["autoScale"].(map[string]any); ok {
				row["minCount"] = as["minCount"]
				row["maxCount"] = as["maxCount"]
			}
		}
		// Flavor name for at-a-glance sizing.
		if mc, ok := np["machineConfiguration"].(map[string]any); ok {
			if fl, ok := mc["flavor"].(map[string]any); ok {
				row["flavor"] = fl["name"]
			}
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"node_pools": out}), false
}

// handleK8sAddNodePool adds a worker node pool to a cluster (gated). Async.
func (s *Registry) handleK8sAddNodePool(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	if clusterID == "" {
		return usageResult(fmt.Errorf("cluster_id is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "k8s_add_nodepool creates billable worker nodes. Call again with confirmed=true after the user approves.",
			"preview": k8sPreview(engine.ResourceK8sNodePool, nodePoolName(args), args),
		}), true
	}

	cli, pid, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	pc, err := cli.ProductConfiguration(ctx)
	if err != nil {
		return errResult(err), true
	}

	// Flavor: explicit name, else smallest non-GPU that fits cpu/ram.
	cpu := intArg(args, "cpu", 2)
	ram := intArg(args, "ram_gb", 4)
	flavorName := stringArg(args, "flavor")
	flavorID := ""
	var bCPU, bRAM int
	for _, f := range pc.Flavors {
		if f.GPU != 0 {
			continue
		}
		if flavorName != "" {
			if f.Name == flavorName {
				flavorID = f.FlavorID
				break
			}
			continue
		}
		if f.CPU >= cpu && f.RAM >= ram {
			if flavorID == "" || f.CPU < bCPU || (f.CPU == bCPU && f.RAM < bRAM) {
				flavorID, bCPU, bRAM = f.FlavorID, f.CPU, f.RAM
			}
		}
	}
	if flavorID == "" {
		return errResult(fmt.Errorf("no matching node flavor (cpu>=%d ram>=%d); see k8s_options", cpu, ram)), true
	}

	// Nodes subnet + security group: explicit, else from the cluster's zone.
	// The zone was the product's default zone and the subnet fell back to
	// the project's first subnet of any zone — the class of L-08 for worker
	// nodes (release audit, L-08 follow-up).
	zoneRef := stringArg(args, "zone")
	if zoneRef == "" {
		// Not the product's default zone when the cluster's is unreadable:
		// that is how the pool would land away from the control plane.
		zid, zerr := clusterZoneID(ctx, cli, clusterID)
		if zerr != nil {
			return errResult(fmt.Errorf("read the zone of cluster %s: %w (or pass zone)", clusterID, zerr)), true
		}
		if zid == "" {
			return errResult(clierrors.New(clierrors.CodeConfigInvalid,
				fmt.Sprintf("cluster %s does not report its zone (controlPlane.zones is empty)", clusterID),
				"Pass zone: the availability zone of the cluster's control plane (k8s_get).")), true
		}
		zoneRef = zid
	}
	zone, err := s.k8sZone(ctx, pc, zoneRef)
	if err != nil {
		return errResult(err), true
	}
	nodesSubnetID, err := s.k8sNodesSubnet(ctx, pid, stringArg(args, "nodes_subnet_id"), zone)
	if err != nil {
		return errResult(err), true
	}
	sgID := stringArg(args, "security_group_id")
	if sgID == "" {
		eng, e := s.authedEngine(ctx, nil)
		if e != nil {
			return errResult(e), true
		}
		// No group found (or the list failed) sent the pool without
		// securityGroupId — the spec takes that — while the description
		// promises a zone-matched SSH-access group; provision refuses the
		// same case (final audit, stage 2).
		sgs, se := eng.Cloud.ListSecurityGroups(ctx, pid)
		if se != nil {
			return errResult(fmt.Errorf("list security groups: %w (or pass security_group_id)", se)), true
		}
		i := provision.PickSecurityGroup(sgs, zone.computeID, zone.name)
		if i < 0 {
			return errResult(clierrors.New(clierrors.CodeConfigInvalid,
				fmt.Sprintf("no security group of the project in zone %s for the node pool; nothing was created", zone.name),
				"Pass security_group_id (list_security_groups), or create a group in this zone in the Cloud.ru console.")), true
		}
		sgID = sgs[i].ID
	}

	count := intArg(args, "count", 2)
	diskSize := intArg(args, "disk_gb", 20)
	name := nodePoolName(args)

	req := mk8s.CreateNodePoolReq{
		ClusterID:            clusterID,
		Name:                 name,
		ScalePolicy:          mk8s.ScalePolicy{FixedScale: &mk8s.FixedScale{Count: count}},
		MachineConfiguration: mk8s.MachineConfigurationRequest{FlavorID: flavorID, Disk: &mk8s.DiskRequest{Size: diskSize}},
		NetworkConfiguration: mk8s.NodePoolNetworkConfigurationRequest{NodesSubnetID: nodesSubnetID, SecurityGroupID: sgID},
		UpdateConfiguration:  mk8s.NodePoolUpdateConfiguration{Strategy: "NODE_POOL_UPDATE_STRATEGY_ROLLING_UPDATE"},
	}
	op, err := cli.CreateNodePool(ctx, req)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"status": "creating", "node_pool": name, "cluster_id": clusterID,
		"operation_id": op.ID, "count": count, "flavor_id": flavorID,
		"note": "Node pool is provisioning (async). Poll k8s_nodepools. NB: if Cloud.ru returns 503 'no healthy upstream' it's a transient backend outage — retry.",
	}), false
}

// nodePoolName is the node pool's name: the argument, else nodepool-1.
// The preview shows the same default the create uses; it showed an empty
// name (release audit, L-10).
func nodePoolName(args map[string]any) string {
	return firstNonEmptyStr(stringArg(args, "name"), "nodepool-1")
}

// k8sArgsError checks the arguments of k8s_plan and k8s_create that need
// no API, before the confirmation gate. master_count=2 used to become 1
// with only a warning (release audit, L-27).
func k8sArgsError(args map[string]any) error {
	if _, ok := args["master_count"]; ok {
		switch n := intArg(args, "master_count", 0); n {
		case 1, 3, 5:
		default:
			return clierrors.New(clierrors.CodeUsage,
				fmt.Sprintf("master_count must be 1, 3 or 5, got %v", args["master_count"]),
				"1 — a zonal control plane; 3 or 5 — a highly available one.")
		}
	}
	return nil
}

// k8sZoneRef is the zone of a new cluster: its name, the zone ID of the
// compute API (subnets carry it) and the ID mk8s takes in zones.
type k8sZoneRef struct {
	name, computeID, mk8sID string
}

// k8sZone resolves the cluster's zone: the 'zone' argument (name or UUID),
// else the default zone of productConfiguration. A zone neither the compute
// API nor mk8s knows, or one mk8s does not offer, is USAGE (release audit,
// L-08).
func (s *Registry) k8sZone(ctx context.Context, pc *mk8s.ProductConfig, ref string) (k8sZoneRef, error) {
	ref = firstNonEmptyStr(ref, pc.DefaultZone)
	if ref == "" {
		return k8sZoneRef{}, clierrors.New(clierrors.CodeUsage, "zone is required: productConfiguration names no default zone",
			"Pass zone, e.g. ru.AZ-2 (k8s_options lists the zones).")
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return k8sZoneRef{}, err
	}
	czones, err := eng.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return k8sZoneRef{}, fmt.Errorf("list availability zones: %w", err)
	}
	var z k8sZoneRef
	for _, cz := range czones {
		if cz.ID == ref || strings.EqualFold(cz.Name, ref) {
			z.name, z.computeID = cz.Name, cz.ID
			break
		}
	}
	offered := make([]string, 0, len(pc.Zones))
	for _, mz := range pc.Zones {
		offered = append(offered, mz.Name)
		if mz.ZoneID == ref || (mz.Name != "" && strings.EqualFold(mz.Name, firstNonEmptyStr(z.name, ref))) {
			z.mk8sID = mz.ZoneID
			z.name = firstNonEmptyStr(z.name, mz.Name)
		}
	}
	// ref may be an mk8s zone ID (controlPlane.zones of a cluster): the
	// compute zone is then found by the name mk8s gives it.
	if z.computeID == "" && z.name != "" {
		for _, cz := range czones {
			if strings.EqualFold(cz.Name, z.name) {
				z.computeID = cz.ID
				break
			}
		}
	}
	hint := "k8s_options lists the zones of Managed Kubernetes."
	if len(offered) > 0 {
		hint = "Zones of Managed Kubernetes: " + strings.Join(offered, ", ") + " (k8s_options)."
	}
	switch {
	case z.computeID == "" && z.mk8sID == "":
		return k8sZoneRef{}, clierrors.New(clierrors.CodeUsage, fmt.Sprintf("availability zone %q not found", ref), hint)
	case z.mk8sID == "" && len(pc.Zones) > 0:
		return k8sZoneRef{}, clierrors.New(clierrors.CodeUsage,
			fmt.Sprintf("zone %s is not offered for Managed Kubernetes", z.name), hint)
	case z.mk8sID == "":
		// The product lists no zones: the compute ID is sent as is. A wrong
		// one is a 400 before anything is created, while no zones at all
		// puts the masters in the API's default zone (L-08).
		z.mk8sID = z.computeID
	case z.computeID == "":
		z.computeID = z.mk8sID
	}
	return z, nil
}

// subnetZoneID is the zone UUID of a project subnet, "" when unknown.
func (s *Registry) subnetZoneID(ctx context.Context, pid, subnetID string) string {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return ""
	}
	subs, err := eng.Cloud.ListSubnets(ctx, pid)
	if err != nil {
		return ""
	}
	for _, sn := range subs {
		if sn.ID == subnetID {
			return sn.AvailabilityZoneID
		}
	}
	return ""
}

// k8sVIPSubnet is the subnet of the control-plane private VIP: subnet_id
// as given, else the zone's main subnet (provision.PickSubnet, as deploy).
// A subnet of another zone is CONFIG_INVALID: the masters went to one zone
// and the VIP to another, and the cluster's nodes stayed NotReady; the
// lookup used to fall back to the project's first subnet of any zone
// (release audit, L-08).
func (s *Registry) k8sVIPSubnet(ctx context.Context, pid, subnetID string, zone k8sZoneRef) (string, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", err
	}
	subs, err := eng.Cloud.ListSubnets(ctx, pid)
	if err != nil {
		return "", fmt.Errorf("list subnets: %w", err)
	}
	if subnetID != "" {
		for _, sn := range subs {
			if sn.ID != subnetID {
				continue
			}
			if !provision.SubnetInZone(sn, zone.computeID, zone.name) {
				return "", clierrors.New(clierrors.CodeConfigInvalid,
					fmt.Sprintf("subnet %s is not in zone %s, where the control plane goes; nothing was created", subnetID, zone.name),
					"Pass a subnet of that zone (vpc_subnets), or pass zone = the subnet's zone.")
			}
			return subnetID, nil
		}
		return "", clierrors.New(clierrors.CodeConfigInvalid, fmt.Sprintf("subnet %s is not in the project", subnetID),
			"Take a subnet ID from vpc_subnets, or omit subnet_id: the zone's main subnet is taken.")
	}
	if i := provision.PickSubnet(subs, zone.computeID, zone.name); i >= 0 {
		return subs[i].ID, nil
	}
	return "", clierrors.New(clierrors.CodeConfigInvalid,
		fmt.Sprintf("no subnet in zone %s for the control-plane VIP; nothing was created", zone.name),
		"Create a subnet in that zone, pass subnet_id of one, or pick another zone (k8s_options).")
}

// clusterZoneID is the zone ID of the cluster's control plane
// (controlPlane.zones[0] of GET /v3/clusters/{id}), "" when the cluster
// does not list one.
func clusterZoneID(ctx context.Context, cli *mk8s.Client, clusterID string) (string, error) {
	m, err := cli.GetCluster(ctx, clusterID)
	if err != nil {
		return "", err
	}
	cp, _ := m["controlPlane"].(map[string]any)
	zones, _ := cp["zones"].([]any)
	if len(zones) == 0 {
		return "", nil
	}
	z, _ := zones[0].(string)
	return z, nil
}

// k8sNodesSubnet is the subnet of a node pool: nodes_subnet_id as given
// (refused when it is in another zone than the pool's), else the zone's
// main subnet; no subnet in the zone is CONFIG_INVALID instead of the
// project's first subnet of any zone.
func (s *Registry) k8sNodesSubnet(ctx context.Context, pid, subnetID string, zone k8sZoneRef) (string, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", err
	}
	subs, err := eng.Cloud.ListSubnets(ctx, pid)
	if err != nil {
		return "", fmt.Errorf("list subnets: %w", err)
	}
	if subnetID != "" {
		for _, sn := range subs {
			if sn.ID == subnetID {
				if !provision.SubnetInZone(sn, zone.computeID, zone.name) {
					return "", clierrors.New(clierrors.CodeConfigInvalid,
						fmt.Sprintf("subnet %s is not in zone %s of the node pool; nothing was created", subnetID, zone.name),
						"Pass a subnet of that zone (vpc_subnets), or pass zone = the subnet's zone.")
				}
				return subnetID, nil
			}
		}
		return "", clierrors.New(clierrors.CodeConfigInvalid, fmt.Sprintf("subnet %s is not in the project", subnetID),
			"Take a subnet ID from vpc_subnets, or omit nodes_subnet_id: the zone's main subnet is taken.")
	}
	if i := provision.PickSubnet(subs, zone.computeID, zone.name); i >= 0 {
		return subs[i].ID, nil
	}
	return "", clierrors.New(clierrors.CodeConfigInvalid,
		fmt.Sprintf("no subnet in zone %s for the node pool; nothing was created", zone.name),
		"Create a subnet in that zone or pass nodes_subnet_id of one.")
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// k8sPreview is the preview of k8s_create and k8s_add_nodepool: the
// arguments as given (the rest is resolved at creation; k8s_plan shows it).
// evo has no rate card for Managed Kubernetes, so there is no price.
func k8sPreview(resource, name string, args map[string]any) engine.CreatePreview {
	p := engine.CreatePreview{Resource: resource, Name: name}
	if z := stringArg(args, "zone"); z != "" {
		if looksUUID(z) {
			p.Zone = &deployplan.RefSpec{ID: z}
		} else {
			p.Zone = &deployplan.RefSpec{Name: z}
		}
	}
	p.Add("cluster_id", stringArg(args, "cluster_id"))
	p.AddArgs(args, "name", "zone", "cluster_id")
	p.Price()
	return p
}
