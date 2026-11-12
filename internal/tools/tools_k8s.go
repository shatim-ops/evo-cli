// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
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
		return usageT("cluster_id is required", "нужен cluster_id"), true
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
			row := map[string]any{"flavor_id": f.FlavorID, "name": f.Name, "cpu": f.CPU, "ram": f.RAM}
			// Only what the catalogue sent (D54, FB15-40).
			if f.Purpose != "" {
				row["purpose"] = f.Purpose
			}
			flavors = append(flavors, row)
		}
	}
	zones := make([]string, 0, len(pc.Zones))
	defaults := map[string]any{"zone": pc.DefaultZone, "channel": pc.DefaultChannel, "disk_type": pc.DefaultDiskType}
	for _, z := range pc.Zones {
		zones = append(zones, z.Name)
		// The default zone is an id; its name is in the same answer (Линия
		// CLI).
		if z.ZoneID == pc.DefaultZone && z.Name != "" {
			defaults["zone_name"] = z.Name
		}
	}
	return toJSON(map[string]any{
		"versions_by_channel": versions,
		"flavors":             flavors,
		"zones":               zones,
		"defaults":            defaults,
	}), false
}

// k8sPlan is a resolved cluster create: the request, the names of its zone
// and flavor, and the warnings of the resolution.
type k8sPlan struct {
	req  mk8s.CreateClusterReq
	zone string
	// subnetName is the VIP subnet's name, read with it (Линия CLI).
	subnetName string
	flavor     mk8s.Flavor
	warnings   []string
}

// resolved is what the plan settles that the arguments may leave open, for
// the preview of k8s_create (k8sPreview).
func (p k8sPlan) resolved() map[string]any {
	cp := p.req.ControlPlaneConfiguration
	return map[string]any{
		"zone": p.zone, "subnet_id": p.req.NetworkConfiguration.PrivateVipSubnetID, "subnet_name": p.subnetName,
		"flavor": p.flavor.Name, "cpu": p.flavor.CPU, "ram_gb": p.flavor.RAM,
		"version": cp.Version, "master_count": cp.SizingConfiguration.MasterCount,
		"kube_api_internet": p.req.NetworkConfiguration.KubeApiInternet,
	}
}

// pickK8sFlavor is the flavor of the masters (k8s_create, k8s_plan) or of
// the nodes (k8s_add_nodepool), one choice for both (FB123-6): want by its
// name, else the smallest without a GPU with cpu and ram, ties by name and
// id whatever the order of the catalogue (D27). An explicit name that is
// not in the catalogue, or one with a GPU, is refused for what it is, not
// as a want of cpu and ram nobody asked (FB123-7). USAGE either way.
func pickK8sFlavor(flavors []mk8s.Flavor, want string, cpu, ram int) (mk8s.Flavor, error) {
	hint := i18n.T("k8s_options lists the flavors.", "Конфигурации — в выводе evo k8s options.")
	if want != "" {
		for _, f := range flavors {
			if f.Name != want {
				continue
			}
			if f.GPU != 0 {
				return mk8s.Flavor{}, clierrors.New(clierrors.CodeUsage,
					i18n.Tf("flavor %q has a GPU: evo runs no masters or nodes on a GPU flavor", "у конфигурации %q есть GPU: evo не ставит на GPU мастера и узлы", want), hint)
			}
			return f, nil
		}
		return mk8s.Flavor{}, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("flavor %q is not in the catalogue of Managed Kubernetes", "конфигурации %q нет в каталоге Managed Kubernetes", want), hint)
	}
	var best *mk8s.Flavor
	for i := range flavors {
		f := &flavors[i]
		if f.GPU != 0 || f.CPU < cpu || f.RAM < ram {
			continue
		}
		if best == nil || cmp.Or(cmp.Compare(f.CPU, best.CPU), cmp.Compare(f.RAM, best.RAM),
			provision.CompareByNameID(f.Name, f.FlavorID, best.Name, best.FlavorID)) < 0 {
			best = f
		}
	}
	if best == nil {
		return mk8s.Flavor{}, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("no k8s flavor without a GPU with cpu>=%d ram>=%d", "нет конфигурации k8s без GPU с cpu>=%d ram>=%d", cpu, ram), hint)
	}
	return *best, nil
}

// resolveK8sPlan resolves the create parameters (version, flavor, zone,
// subnet, SA).
func (s *Registry) resolveK8sPlan(ctx context.Context, args map[string]any) (k8sPlan, error) {
	var warnings []string
	cli, pid, saID, err := s.k8sClient(ctx)
	if err != nil {
		return k8sPlan{}, err
	}
	pc, err := cli.ProductConfiguration(ctx)
	if err != nil {
		return k8sPlan{}, err
	}

	// Version: explicit (one of a channel's, D54), else newest STABLE,
	// else first available.
	version := stringArg(args, "version")
	var channelVersions []string
	for _, ch := range pc.Channels {
		channelVersions = append(channelVersions, ch.AvailableVersions...)
	}
	if err := notInCatalogue(i18n.T("Kubernetes version", "версии Kubernetes"), version, channelVersions, "k8s_options"); err != nil {
		return k8sPlan{}, err
	}
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

	flavor, err := pickK8sFlavor(pc.Flavors, stringArg(args, "flavor"), intArg(args, "cpu", 2), intArg(args, "ram_gb", 4))
	if err != nil {
		return k8sPlan{}, err
	}

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
		return k8sPlan{}, err
	}
	subnetID, subnetName, err := s.k8sVIPSubnet(ctx, pid, stringArg(args, "subnet_id"), zone)
	if err != nil {
		return k8sPlan{}, err
	}

	// Cluster service account: explicit, else the agent's own (token sub).
	clusterSaID := firstNonEmptyStr(stringArg(args, "cluster_sa_id"), saID)
	if clusterSaID == "" {
		return k8sPlan{}, errors.New(i18n.T("could not resolve cluster service account id; pass cluster_sa_id", "не удалось определить ID сервисного аккаунта кластера; передайте cluster_sa_id"))
	}

	name := k8sClusterNameArg(args)

	kubeApiInternet := true
	if v, ok := args["kube_api_internet"].(bool); ok {
		kubeApiInternet = v
	}

	req := mk8s.CreateClusterReq{
		Name:      name,
		ProjectID: pid,
		ControlPlaneConfiguration: mk8s.ControlPlaneConfiguration{
			Version:             version,
			SizingConfiguration: mk8s.SizingConfiguration{MasterCount: masterCount, FlavorID: flavor.FlavorID},
			// Without zones the API puts the masters in its default zone
			// (ru.AZ-2), away from the VIP subnet of another zone: the
			// cluster's nodes stay NotReady (release audit, L-08).
			Zones: []string{zone.mk8sID},
		},
		NetworkConfiguration:  mk8s.NetworkConfiguration{PrivateVipSubnetID: subnetID, KubeApiInternet: kubeApiInternet},
		IdentityConfiguration: mk8s.IdentityConfiguration{ClusterSaID: clusterSaID},
	}
	return k8sPlan{req: req, zone: zone.name, subnetName: subnetName, flavor: flavor, warnings: warnings}, nil
}

// handleK8sPlan resolves and shows the cluster config without creating it.
func (s *Registry) handleK8sPlan(ctx context.Context, args map[string]any) (string, bool) {
	if err := k8sArgsError(args); err != nil {
		return errResult(err), true
	}
	plan, err := s.resolveK8sPlan(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	req := plan.req
	return toJSON(map[string]any{
		"name":              req.Name,
		"zone":              plan.zone,
		"version":           req.ControlPlaneConfiguration.Version,
		"master_count":      req.ControlPlaneConfiguration.SizingConfiguration.MasterCount,
		"flavor":            plan.flavor.Name,
		"flavor_id":         req.ControlPlaneConfiguration.SizingConfiguration.FlavorID,
		"subnet_id":         req.NetworkConfiguration.PrivateVipSubnetID,
		"subnet_name":       plan.subnetName,
		"cluster_sa_id":     req.IdentityConfiguration.ClusterSaID,
		"kube_api_internet": req.NetworkConfiguration.KubeApiInternet,
		"warnings":          plan.warnings,
		// The Russian note is the CLI's (MCP speaks English, D6): the
		// commands of evo, not the tools (dogfood №4).
		"note": i18n.T("Read-only plan. On the user's approval call k8s_create with the same args + confirmed=true. Cluster creation is async (5-10 min per the docs, 7-12 seen live); poll k8s_get. Add workers with k8s_add_nodepool.",
			"План только для чтения: кластер не создан. Создать — evo k8s create с теми же флагами (без терминала — с --yes). Кластер создаётся асинхронно: 5–10 мин по документации, вживую 7–12; статус — evo k8s get <ID кластера>. Рабочие узлы — evo k8s add-nodepool <ID кластера>."),
	}), false
}

// planK8sCreate plans the create of a cluster (gated). Async.
func (s *Registry) planK8sCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	if err := k8sArgsError(args); err != nil {
		return nil, err
	}
	// A second cluster of the same name is a second bill: refused before
	// the consent, as every other create (DECISIONS D21).
	name := k8sClusterNameArg(args)
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return nil, err
	}
	list := func(ctx context.Context) ([]map[string]any, error) {
		clusters, err := cli.ListClusters(ctx)
		listed := make([]map[string]any, 0, len(clusters))
		for _, c := range clusters {
			listed = append(listed, map[string]any{"id": c.ID, "name": c.Name, "status": c.Status})
		}
		return listed, err
	}
	listed, err := list(ctx)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("check the name is free: list clusters: %w", "проверка, что имя свободно: список кластеров: %w"), err)
	}
	if id, st, ok := takenName(listed, name); ok {
		return answered(nameTakenResult(i18n.T("Kubernetes cluster", "кластер Kubernetes"), name, id, st, "k8s_get cluster_id="+id)), nil
	}
	// The plan before the consent: the preview names the cluster that is
	// made — the masters' flavor, version, zone, VIP subnet — and a plan
	// that cannot be resolved is refused before the question (D29,
	// FB123-5).
	plan, err := s.resolveK8sPlan(ctx, args)
	if err != nil {
		return nil, err
	}
	// The preview is built for the question only: its price is a request
	// a consented call does not make.
	preview := func(ctx context.Context) (map[string]any, error) {
		preview, err := s.completePreview(ctx, nil, k8sPreview(engine.ResourceK8sCluster, name, args, plan.resolved()))
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"message": i18n.T("k8s_create builds a real (billable) Kubernetes cluster.",
				"k8s_create создаёт настоящий (платный) кластер Kubernetes."),
			"preview": preview,
		}, nil
	}
	return &Plan{PreviewOf: preview, Execute: func(ctx context.Context) (string, bool) {
		req := plan.req
		op, err := cli.CreateCluster(ctx, req)
		if err != nil || op.ID == "" {
			if err == nil {
				err = noID("operation")
			}
			return lostCreate(ctx, "k8s_create", list, i18n.T("Kubernetes cluster", "кластер Kubernetes"), name, nil, "k8s_get cluster_id", err)
		}
		return toJSON(map[string]any{
			"status":       "creating",
			"cluster_id":   op.ResourceID,
			"operation_id": op.ID,
			"name":         req.Name,
			"note": i18n.T("Cluster is provisioning (async, about 10 min). Poll k8s_get with this cluster_id until status is running, then k8s_kubeconfig.",
				fmt.Sprintf("Кластер создаётся (асинхронно, около 10 мин). Проверяйте evo k8s get %[1]s, пока статус не станет running, затем — evo k8s kubeconfig %[1]s.", op.ResourceID)),
		}), false
	}}, nil
}

// handleK8sKubeconfig returns the kubeconfig for a cluster.
func (s *Registry) handleK8sKubeconfig(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageT("cluster_id is required", "нужен cluster_id"), true
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
		return errResult(fmt.Errorf(i18n.T("embed_token: OIDC token exchange failed: %w", "embed_token: не удалось получить токен OIDC: %w"), e)), true
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

// planK8sDelete plans the delete of a cluster (gate.go). Async.
func (s *Registry) planK8sDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return nil, usageErr("cluster_id is required", "нужен cluster_id")
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return nil, err
	}
	return s.deletePlan("k8s_delete", "k8s_delete permanently deletes cluster %s.",
		"k8s_delete безвозвратно удаляет кластер %s.",
		id, func() (map[string]any, error) { return cli.GetCluster(ctx, id) },
		func(ctx context.Context) (string, bool) {
			op, err := cli.DeleteCluster(ctx, id)
			if err != nil {
				return s.deleteFailed("k8s_delete", id, err)
			}
			// No k8s_operation: k8s_get shows the cluster until it is gone.
			w := opWatch("k8s_delete", id, map[string]any{"cluster_id": id}, op.ID, "", cli.GetOperation)
			w.next, w.nextArgs = "k8s_get", map[string]any{"cluster_id": id}
			return deleteOutcome(ctx, args, w)
		}), nil
}

// handleK8sOperation polls an operation of Managed Kubernetes by id.
func (s *Registry) handleK8sOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageT("'operation_id' is required", "нужен 'operation_id'"), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	return s.operation(ctx, "k8s_operation", args, func(ctx context.Context) (any, error) { return cli.GetOperation(ctx, id) })
}

// handleK8sNodePools lists node pools of a cluster. The API has no list
// across clusters: GET /v3/nodePools requires clusterId (the tool used to
// call it without one and got API_ERROR, cloud-e2e readonly #23).
func (s *Registry) handleK8sNodePools(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	if clusterID == "" {
		return errResult(clierrors.New(clierrors.CodeUsage, i18n.T("cluster_id is required: the API lists node pools of one cluster", "нужен cluster_id: API выдаёт группы узлов одного кластера"),
			i18n.T("Find the cluster ID with k8s_list (evo k8s list).", "ID кластера — в выводе evo k8s list."))), true
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

// handleK8sNodePoolGet reads a node pool by its id: what the list of its
// cluster does not say — the cluster, the zone, the disk, the network, the
// labels and taints of its nodes — and the status an operation of the pool
// is read with (operationReads).
func (s *Registry) handleK8sNodePoolGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "nodepool_id")
	if id == "" {
		return usageT("nodepool_id is required", "нужен nodepool_id"), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	np, err := cli.GetNodePool(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	card := nodePoolCard(np)
	poolClusterName(ctx, cli, card, card)
	// The subnet and the security group of the nodes stay ids: their names
	// are two more reads, of two other services, at every call — and an
	// operation of a pool reads this card at every poll (operationReads).
	card["note"] = i18n.T("nodes_subnet_id and security_group_id are ids; list_subnets and list_security_groups name them.",
		"nodes_subnet_id и security_group_id — id; имена — в выводе evo vpc subnets и evo vpc security-groups.")
	return toJSON(card), false
}

// Statuses of a node pool, as the mk8s API names them (NodePool.status).
const (
	poolRunning     = "OBJECT_STATUS_RUNNING"
	poolScalingUp   = "OBJECT_STATUS_SCALING_UP"
	poolScalingDown = "OBJECT_STATUS_SCALING_DOWN"
	poolError       = "OBJECT_STATUS_ERROR"
)

// poolSettling are the statuses another change put a pool in and the
// platform takes it out of on its own: a scale waits for them to settle
// before it decides (D99, П4), as a power tool does for a VM.
var poolSettling = map[string]bool{
	"OBJECT_STATUS_PENDING": true, "OBJECT_STATUS_PROVISIONING": true, "OBJECT_STATUS_RESUMING": true,
	poolScalingUp: true, poolScalingDown: true, "OBJECT_STATUS_STOPPING": true, "OBJECT_STATUS_SUSPENDING": true,
	"OBJECT_STATUS_UPGRADING": true, "OBJECT_STATUS_EDITING": true, "OBJECT_STATUS_ADDING": true,
}

// poolGoing are the statuses of a pool there is nothing to scale in: it is
// being deleted, or is deleted.
var poolGoing = map[string]bool{"OBJECT_STATUS_DELETING": true, "OBJECT_STATUS_DELETED": true}

// maxPoolCount is the largest count a request of mk8s carries: the
// specification declares FixedScale.count an int32. A bound of the
// request, not of what the platform accepts; the schema of the tool
// refuses a count beyond it before the planner (D36) — over MCP a JSON
// number of any size arrives, and int(float64) of it is another number.
const maxPoolCount = math.MaxInt32

// poolSettleTimeout bounds the wait before the question for a pool in a
// transient status of another change, as a VM's (D53: 30 s).
const poolSettleTimeout = 30 * time.Second

// scaleWait is how long, in seconds, a scale waits for its operation, as a
// delete does (D74). A variable only so that tests can shorten it.
var scaleWait = opWaitMaxSeconds

// poolClusterName writes into out the cluster of the pool card by its
// name, cluster_name, next to its id — as a person knows it. A cluster
// that is not read leaves out as it is: the id is there.
func poolClusterName(ctx context.Context, cli *mk8s.Client, card, out map[string]any) {
	cid, _ := card["cluster_id"].(string)
	if cid == "" {
		return
	}
	if c, err := cli.GetCluster(ctx, cid); err == nil {
		if name, _ := c["name"].(string); name != "" {
			out["cluster_name"] = name
		}
	}
}

// poolScale is the scale of a pool as its card reads: the count of a fixed
// policy, or that the platform scales the pool itself.
func poolScale(card map[string]any) (desired int, fixed bool) {
	if card["scale"] != "fixed" {
		return 0, false
	}
	n, _ := card["desired"].(float64)
	return int(n), true
}

// planK8sNodePoolScale is the planner of k8s_nodepool_scale (gate.go; D99,
// П1–П6; docs/internal/briefs/k8s-nodepools.md): it reads the pool before
// the question and answers by what it reads — the pool has the count (a
// repeat: success with no question and no write), is on its way to it
// (waited for with no second request), cannot be scaled (refused before
// the question), or the preview of the request.
func (s *Registry) planK8sNodePoolScale(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "nodepool_id")
	if id == "" {
		return nil, usageErr("nodepool_id is required", "нужен nodepool_id")
	}
	if _, ok := args["count"]; !ok {
		return nil, usageErr("count is required", "нужен count")
	}
	// The bounds of count are the schema's (D36): 0 to maxPoolCount.
	count := intArg(args, "count", 0)
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return nil, err
	}
	// The engine only for the timings of the waits: the pool is read and
	// changed by the mk8s client.
	eng, err := s.localEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	np, err := cli.GetNodePool(ctx, id)
	if err != nil {
		return nil, err
	}
	card := nodePoolCard(np)
	name, _ := card["name"].(string)
	// The pool by its name in the preview only; a refusal names it by the
	// id the caller gave, so no text of the cloud is in evo's own words.
	label := fmt.Sprintf("%s (%s)", quoteCloud(name), id)
	status, _ := card["status"].(string)
	desired, fixed := poolScale(card)
	if card["scale"] == nil {
		// Neither policy in the answer: evo does not guess the count the
		// pool has.
		return nil, clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("node pool %s: the answer of the platform names no scale policy, so evo cannot tell the number of nodes it has; nothing was sent", "группа узлов %s: в ответе платформы нет политики масштаба — сколько в группе узлов, evo сказать не может; ничего не отправлено", id),
			i18n.T("k8s_nodepool_get shows the pool as the platform gives it.", "Группу, как её отдаёт платформа, показывает evo k8s nodepool-get."))
	}
	if _, isNumber := card["desired"].(float64); fixed && !isNumber {
		// Not read as zero: the preview would count nodes from a number
		// the pool does not have.
		return nil, clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("node pool %s: the count in the answer of the platform is not a number, so evo cannot tell the number of nodes it has; nothing was sent", "группа узлов %s: число узлов в ответе платформы — не число; сколько в группе узлов, evo сказать не может; ничего не отправлено", id),
			i18n.T("k8s_nodepool_get shows the pool as the platform gives it.", "Группу, как её отдаёт платформа, показывает evo k8s nodepool-get."))
	}
	if !fixed {
		// The platform chooses the count of an autoscaled pool; evo does
		// not change the policy of a pool.
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("node pool %s is autoscaled (min %v, max %v): the platform chooses its number of nodes; nothing was sent", "группа узлов %s с автомасштабом (от %v до %v узлов): число узлов выбирает платформа; ничего не отправлено", id, card["min_count"], card["max_count"]),
			i18n.T("evo scales a pool with a fixed count and does not change the scale policy of a pool. k8s_nodepool_get shows the pool.", "evo меняет число узлов только у группы с фиксированным масштабом и политику масштаба не меняет. Группу показывает evo k8s nodepool-get."))
	}
	// On its way to this very count: this call's own request, cut before
	// its end, or another caller's. Nothing is sent, so no question.
	onItsWay := func(st string, want int) bool {
		return (st == poolScalingUp || st == poolScalingDown) && want == count
	}
	if poolSettling[status] && !onItsWay(status, desired) {
		last, timedOut, atLimit, werr := waitState(ctx, eng, poolSettleTimeout,
			func(ctx context.Context) (string, bool, error) {
				now, err := cli.GetNodePool(ctx, id)
				switch {
				case err != nil && apiStatus(err) == http.StatusNotFound:
					return "", false, err
				case err != nil:
					return "", false, nil
				}
				np = now
				st, _ := now["status"].(string)
				return st, true, nil
			},
			func(st string) string {
				return fmt.Sprintf(i18n.T("node pool %s: %s, waiting…", "группа узлов %s: %s, жду…"), id, quoteCloud(st))
			},
			func(st string) (bool, error) { return !poolSettling[st], nil })
		switch {
		case werr != nil:
			return nil, werr
		case atLimit:
			ans := map[string]any{"status": "waiting", "nodepool_id": id, "name": name, "state": last}
			ans["next"] = fmt.Sprintf("Node pool %s is %s at the time limit of an MCP call; it cannot be scaled until its status settles. Call k8s_nodepool_scale again when it settles: it shows the preview and asks before it sends the request.", id, quoteCloud(last))
			nextSameCall(ans, "k8s_nodepool_scale", args)
			return answered(toJSON(ans), false), nil
		case timedOut > 0:
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("node pool %s is in status %s: it cannot be scaled until the status settles", "группа узлов %s в состоянии %s: менять число узлов можно, когда состояние устоится", id, quoteCloud(last)),
				i18n.T("Repeat the call when k8s_nodepool_get shows the pool running.", "Повторите вызов, когда evo k8s nodepool-get покажет группу в OBJECT_STATUS_RUNNING."))
		}
		card = nodePoolCard(np)
		status = last
		desired, fixed = poolScale(card)
		if !fixed {
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("node pool %s is autoscaled now: the platform chooses its number of nodes; nothing was sent", "группа узлов %s теперь с автомасштабом: число узлов выбирает платформа; ничего не отправлено", id),
				i18n.T("k8s_nodepool_get shows the pool.", "Группу показывает evo k8s nodepool-get."))
		}
	}
	switch {
	case status == poolRunning && desired == count:
		return answered(toJSON(poolAnswer(card, "already_scaled", count)), false), nil
	case onItsWay(status, desired):
		ans := poolAnswer(card, "scaling", count)
		ans["next"] = fmt.Sprintf("Node pool %s is on its way to %d nodes (%s). k8s_nodepool_get shows its status.", id, count, quoteCloud(status))
		nextStep(ans, "k8s_nodepool_get", map[string]any{"nodepool_id": id})
		return answered(toJSON(ans), false), nil
	case poolGoing[status]:
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("node pool %s is in status %s: there is nothing to scale", "группа узлов %s в состоянии %s: менять число узлов не у чего", id, quoteCloud(status)),
			i18n.T("k8s_nodepool_get shows the pool; what to do with it is the person's decision.", "Группу показывает evo k8s nodepool-get; что с ней делать — решает человек."))
	case desired == count:
		// The count is the pool's already and the pool is not running:
		// there is no change to ask for, and a request of none is not sent.
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("node pool %s has %d node(s) already and is in status %s, not running: there is no change of the count to make; nothing was sent", "в группе узлов %[1]s уже %[2]d узл., и она в состоянии %[3]s, а не работает: менять число узлов не на что; ничего не отправлено", id, count, quoteCloud(status)),
			i18n.T("k8s_nodepool_get shows the pool; what to do with it is the person's decision.", "Группу показывает evo k8s nodepool-get; что с ней делать — решает человек."))
	case status != poolRunning && count > desired:
		// A departure from П4: more nodes are billable nodes added to a
		// pool that is not working. Fewer nodes go on to the preview — it
		// is the way to stop paying for such a pool.
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("node pool %s is in status %s, not running: evo does not add billable nodes to it; nothing was sent", "группа узлов %s в состоянии %s, а не работает: evo не добавляет в неё платные узлы; ничего не отправлено", id, quoteCloud(status)),
			i18n.T("k8s_nodepool_get shows the pool; what to do with it is the person's decision.", "Группу показывает evo k8s nodepool-get; что с ней делать — решает человек."))
	}

	// What the request sends and what it changes: the count, and the pool
	// as it was read before the question.
	will := map[string]any{"nodepool_id": id, "name": name, "pool_status": status, "count": count, "count_now": desired}
	for _, k := range []string{"cluster_id", "zone", "flavor", "cpu", "ram_gb", "disk_gb"} {
		if v, ok := card[k]; ok {
			will[k] = v
		}
	}
	poolClusterName(ctx, cli, card, will)
	preview := map[string]any{"preview": will}
	switch {
	case count > desired:
		add := count - desired
		will["nodes_added"] = add
		if cpu, ok := card["cpu"].(float64); ok {
			will["cpu_added"] = add * int(cpu)
		}
		if ram, ok := card["ram_gb"].(float64); ok {
			will["ram_gb_added"] = add * int(ram)
		}
		preview["message"] = i18n.Tf("k8s_nodepool_scale adds %d billable node(s) to node pool %s: %d → %d.",
			"k8s_nodepool_scale добавляет платные узлы в группу %[2]s: %[1]d шт., было %[3]d, станет %[4]d.", add, label, desired, count)
		// No price without a probe of the price: the class and where the
		// rates are (CLAUDE.md §6, axis 4).
		will["price_classes"] = []engine.ResourcePrice{{Resource: engine.ResourceK8sNodePool, Class: engine.PricePaidUnpriced,
			Note: i18n.T("price unknown, the resource is billable: the nodes of a pool are billed for their flavor and disk by the rates at tariff_url",
				"цена неизвестна, ресурс платный: узлы группы тарифицируются по своей конфигурации и диску, тарифы — по tariff_url")}}
		will["tariff_url"] = engine.TariffURL(engine.ResourceK8sNodePool)
	default:
		will["nodes_removed"] = desired - count
		preview["message"] = i18n.Tf("k8s_nodepool_scale deletes %d node(s) of node pool %s: %d → %d. The nodes are deleted with the data on their disks, and their pods are evicted; which nodes go, the platform chooses.",
			"k8s_nodepool_scale удаляет узлы группы %[2]s: %[1]d шт., было %[3]d, станет %[4]d. Узлы удаляются вместе с данными на их дисках, поды на них вытесняются; какие узлы уйдут, выбирает платформа.", desired-count, label, desired, count)
		if count == 0 {
			preview["warning"] = i18n.T("the pool is left with no nodes: its pods have nowhere to run; the pool itself stays",
				"в группе не останется узлов: её подам негде работать; сама группа остаётся")
		}
	}
	if status != poolRunning {
		// Not refused: whether the platform takes the request in this
		// status is not known (П4).
		preview["warning"] = i18n.Tf("the pool is in status %s, not running: whether the platform accepts the scale is unknown",
			"группа в состоянии %s, а не работает: примет ли платформа изменение — неизвестно", quoteCloud(status))
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		op, err := cli.ScaleNodePool(ctx, id, count)
		if err != nil || op.ID == "" {
			if err == nil {
				err = noID("operation")
			}
			return s.poolScaleFailed(ctx, cli, id, count, fmt.Errorf(i18n.T("scale node pool: %w", "изменение числа узлов группы: %w"), err))
		}
		ans := poolAnswer(card, "scaling", count)
		ans["operation_id"] = op.ID
		m, werr := waitForOpJSON(ctx, scaleWait, func(ctx context.Context) (map[string]any, error) {
			o, err := cli.GetOperation(ctx, op.ID)
			if err != nil {
				return nil, err
			}
			return opToMap(o), nil
		})
		if errors.Is(werr, context.Canceled) {
			// The scale is started: the answer names it.
			return errAnswer(werr, ans), true
		}
		if werr == nil && m != nil {
			if msg, code, failed := opFailure(m["error"]); failed {
				// The words of the platform are data: quoted on one line in
				// the error, and as they are in details, marked untrusted.
				details := map[string]any{"cause": "operation", "platform_message": msg}
				if code != nil {
					details["platform_code"] = code
				}
				return errAnswer(clierrors.New(clierrors.CodeAPIError,
					i18n.Tf("the scale of node pool %s failed: its operation %s ended with the error %s", "изменение числа узлов группы %s не удалось: операция %s завершилась ошибкой %s", id, op.ID, quoteCloud(fmt.Sprint(msg))),
					i18n.T("k8s_nodepool_get shows the pool as it is now.", "Группу, как она есть сейчас, показывает evo k8s nodepool-get.")),
					map[string]any{"nodepool_id": id, "operation_id": op.ID, "details": details}), true
			}
			if done, _ := m["done"].(bool); done {
				ans["status"] = "scaled"
				return toJSON(ans), false
			}
		}
		ans["next"] = fmt.Sprintf("k8s_operation operation_id=%s shows when the scale ends", op.ID)
		nextStep(ans, "k8s_operation", map[string]any{"operation_id": op.ID, "wait_seconds": opWaitMaxSeconds})
		return toJSON(ans), false
	}}, nil
}

// poolScaleFailed answers a scale whose request failed (П6). A lost
// answer may be a request taken: the pool is read again — it has the
// count or is on its way to it, and the call answers as if the request
// were taken; it is gone, not found; else the error with the status the
// pool is in.
func (s *Registry) poolScaleFailed(ctx context.Context, cli *mk8s.Client, id string, count int, cause error) (string, bool) {
	if ctx.Err() != nil {
		return errResult(cause), true
	}
	now, err := cli.GetNodePool(ctx, id)
	if err != nil {
		if apiStatus(err) == http.StatusNotFound {
			return errResult(err), true
		}
		return errResult(cause), true
	}
	card := nodePoolCard(now)
	status, _ := card["status"].(string)
	desired, fixed := poolScale(card)
	// Only a request that may have been taken — an answer lost, a failure
	// of the server — is told by the count the pool has now: a refusal is
	// a refusal, whoever set that count.
	if fixed && desired == count && !definiteRefusal(cause) {
		if status == poolRunning {
			return toJSON(poolAnswer(card, "scaled", count)), false
		}
		ans := poolAnswer(card, "scaling", count)
		ans["next"] = fmt.Sprintf("Node pool %s is on its way to %d nodes (%s). k8s_nodepool_get shows its status.", id, count, quoteCloud(status))
		nextStep(ans, "k8s_nodepool_get", map[string]any{"nodepool_id": id})
		return toJSON(ans), false
	}
	return errAnswer(cause, map[string]any{"nodepool_id": id, "state": status}), true
}

// planK8sNodePoolDelete is the planner of k8s_nodepool_delete (gate.go;
// D99, docs/internal/briefs/k8s-nodepools.md): the delete of every *_delete
// (deletePlan, deleteOutcome), with what goes with a pool in its preview —
// the cluster, the nodes, their flavor and disk — and whether the cluster
// is left with no pool.
func (s *Registry) planK8sNodePoolDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	const tool = "k8s_nodepool_delete"
	id := stringArg(args, "nodepool_id")
	if id == "" {
		return nil, usageErr("nodepool_id is required", "нужен nodepool_id")
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return nil, err
	}
	var card map[string]any
	readPool := func(ctx context.Context) (map[string]any, error) {
		np, err := cli.GetNodePool(ctx, id)
		if err != nil {
			return nil, err
		}
		return nodePoolCard(np), nil
	}
	p := s.deletePlan(tool,
		"k8s_nodepool_delete permanently deletes node pool %s: its nodes are deleted with the data on their disks, and their pods are evicted.",
		"k8s_nodepool_delete безвозвратно удаляет группу узлов %s: её узлы удаляются вместе с данными на их дисках, поды на них вытесняются.",
		id, func() (m map[string]any, err error) {
			card, err = readPool(ctx)
			return card, err
		},
		func(ctx context.Context) (string, bool) {
			op, err := cli.DeleteNodePool(ctx, id)
			if err != nil {
				return s.deleteFailed(tool, id, err)
			}
			ident := map[string]any{"nodepool_id": id}
			if op.ID == "" {
				// No operation to read: the pool itself, until it is gone.
				return deleteOutcome(ctx, args, goneWatch(tool, id, ident, "", "k8s_nodepool_get", readPool))
			}
			return deleteOutcome(ctx, args, opWatch(tool, id, ident, op.ID, "k8s_operation", cli.GetOperation))
		})
	if p.Execute == nil {
		return p, nil
	}

	// What goes with the pool, as it was read before the question.
	will := map[string]any{}
	for _, k := range []string{"cluster_id", "zone", "flavor", "cpu", "ram_gb", "disk_gb", "disk_type", "min_count", "max_count"} {
		if v, ok := card[k]; ok {
			will[k] = v
		}
	}
	if n, fixed := poolScale(card); fixed {
		will["nodes"] = n
	}
	poolClusterName(ctx, cli, card, will)
	// The other pools of the cluster: a pool that is going is not one the
	// cluster keeps. A list that is not read refuses the call — the
	// preview would not know what the cluster is left with.
	cid, _ := card["cluster_id"].(string)
	pools, err := cli.ListNodePools(ctx, cid)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError,
			i18n.Tf("node pool %s: the node pools of its cluster could not be read, so evo cannot tell whether it is the last one; nothing was sent", "группа узлов %s: список групп её кластера не прочитан — последняя ли она, evo сказать не может; ничего не отправлено", id),
			i18n.T("Repeat the call; k8s_nodepools shows the pools of the cluster.", "Повторите вызов; группы кластера показывает evo k8s nodepools."), err)
	}
	last := true
	for _, o := range pools {
		oid, _ := o["id"].(string)
		st, _ := o["status"].(string)
		if oid != id && !poolGoing[st] {
			last = false
		}
	}
	will["last_pool"] = last
	var warnings []string
	if w, _ := p.Preview["warning"].(string); w != "" {
		warnings = append(warnings, w)
	} else if status, _ := card["status"].(string); status != poolRunning {
		// Not refused: whether the platform takes the delete in this
		// status is not known (П4).
		warnings = append(warnings, i18n.Tf("the pool is in status %s, not running: whether the platform accepts the delete is unknown",
			"группа в состоянии %s, а не работает: примет ли платформа удаление — неизвестно", quoteCloud(status)))
	}
	if last {
		warnings = append(warnings, i18n.T("the cluster is left with no node pool: its pods have nowhere to run; the cluster itself stays, and so does what it is billed for",
			"в кластере не останется групп узлов: его подам негде работать; сам кластер остаётся, как и плата за него"))
	}
	if len(warnings) > 0 {
		p.Preview["warning"] = strings.Join(warnings, "; ")
	}
	p.Preview["preview"] = will
	// What the workloads of the pool made in the project is not read.
	p.Preview["note"] = i18n.T("evo deletes the node pool only: what its workloads created in the project (disks of volumes, load balancers) is not checked",
		"evo удаляет только группу узлов: то, что её нагрузки создали в проекте (диски томов, балансировщики), не проверяется")
	return p, nil
}

// poolAnswer is the answer of a scale: its status and the pool — id, name,
// the count asked for and the status the pool was read in. The cluster of
// the pool is the preview's to name.
func poolAnswer(card map[string]any, status string, count int) map[string]any {
	ans := map[string]any{"status": status, "nodepool_id": card["id"], "count": count}
	for from, to := range map[string]string{"name": "name", "status": "state"} {
		if v, ok := card[from]; ok {
			ans[to] = v
		}
	}
	return ans
}

// nodePoolCard is a node pool of the mk8s API (NodePool, v3) as
// k8s_nodepool_get answers it. Fields the API did not give are left out.
func nodePoolCard(np map[string]any) map[string]any {
	sub := func(m map[string]any, keys ...string) map[string]any {
		for _, k := range keys {
			m, _ = m[k].(map[string]any)
		}
		return m
	}
	out := map[string]any{}
	put := func(key string, v any) {
		if v != nil {
			out[key] = v
		}
	}
	put("id", np["id"])
	put("cluster_id", np["clusterId"])
	put("name", np["name"])
	put("zone", np["zone"])
	put("version", np["version"])
	put("status", np["status"])
	if fs := sub(np, "scalePolicy", "fixedScale"); fs != nil {
		out["scale"] = "fixed"
		put("desired", fs["count"])
	} else if as := sub(np, "scalePolicy", "autoScale"); as != nil {
		out["scale"] = "auto"
		put("min_count", as["minCount"])
		put("max_count", as["maxCount"])
	}
	fl := sub(np, "machineConfiguration", "flavor")
	put("flavor", fl["name"])
	put("cpu", fl["cpu"])
	put("ram_gb", fl["ram"])
	disk := sub(np, "machineConfiguration", "disk")
	put("disk_gb", disk["size"])
	put("disk_type", disk["typeName"])
	nc := sub(np, "networkConfiguration")
	put("nodes_subnet_id", nc["nodesSubnetId"])
	put("security_group_id", nc["securityGroupId"])
	put("auto_repair", sub(np, "autoRepair")["enabled"])
	put("labels", sub(np, "labels")["labels"])
	put("taints", sub(np, "taints")["taints"])
	put("upgrade_versions", sub(np, "versionUpgrade")["availableVersions"])
	return out
}

// planK8sAddNodePool plans a worker node pool of a cluster (gated). Async.
func (s *Registry) planK8sAddNodePool(ctx context.Context, args map[string]any) (*Plan, error) {
	clusterID := stringArg(args, "cluster_id")
	if clusterID == "" {
		return answered(usageT("cluster_id is required", "нужен cluster_id"), true), nil
	}
	cli, pid, _, err := s.k8sClient(ctx)
	if err != nil {
		return nil, err
	}
	// A pool of the name in the cluster: a repeat after a lost answer made
	// a second billable pool (fault matrix, process audit 1.2.1).
	name := nodePoolName(args)
	list := func(ctx context.Context) ([]map[string]any, error) {
		return cli.ListNodePools(ctx, clusterID)
	}
	listed, err := list(ctx)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("check the name is free: list node pools: %w", "проверка, что имя свободно: список групп узлов: %w"), err)
	}
	if id, st, ok := takenName(listed, name); ok {
		return answered(nameTakenResult(i18n.T("node pool", "группа узлов"), name, id, st, "k8s_nodepools cluster_id="+clusterID)), nil
	}
	// The nodes' flavor before the consent, as the masters' (D29): an
	// explicit one with a GPU or not in the catalogue is refused before
	// the question, and the preview names the one chosen.
	pc, err := cli.ProductConfiguration(ctx)
	if err != nil {
		return nil, err
	}
	flavor, err := pickK8sFlavor(pc.Flavors, stringArg(args, "flavor"), intArg(args, "cpu", 2), intArg(args, "ram_gb", 4))
	if err != nil {
		return nil, err
	}
	// Nodes subnet + security group: explicit, else from the cluster's zone,
	// before the consent as the flavor (A12, D29): the preview names them,
	// and a pool that cannot get them is refused before the question.
	// The zone was the product's default zone and the subnet fell back to
	// the project's first subnet of any zone — the class of L-08 for worker
	// nodes (release audit, L-08 follow-up).
	zoneRef := stringArg(args, "zone")
	if zoneRef == "" {
		// Not the product's default zone when the cluster's is unreadable:
		// that is how the pool would land away from the control plane.
		zid, zerr := clusterZoneID(ctx, cli, clusterID)
		if zerr != nil {
			return nil, fmt.Errorf(i18n.T("read the zone of cluster %s: %w (or pass zone)", "чтение зоны кластера %s: %w (или передайте zone)"), clusterID, zerr)
		}
		if zid == "" {
			return nil, clierrors.New(clierrors.CodeConfigInvalid,
				i18n.Tf("cluster %s does not report its zone (controlPlane.zones is empty)", "кластер %s не сообщает свою зону (controlPlane.zones пуст)", clusterID),
				i18n.T("Pass zone: the availability zone of the cluster's control plane (k8s_get).", "Передайте --zone — зону доступности мастер-узлов кластера (evo k8s get <ID кластера>)."))
		}
		zoneRef = zid
	}
	zone, err := s.k8sZone(ctx, pc, zoneRef)
	if err != nil {
		return nil, err
	}
	nodesSubnetID, nodesSubnetName, err := s.k8sNodesSubnet(ctx, pid, stringArg(args, "nodes_subnet_id"), zone)
	if err != nil {
		return nil, err
	}
	sgID, sgName := stringArg(args, "security_group_id"), ""
	if sgID == "" {
		eng, e := s.authedEngine(ctx, nil)
		if e != nil {
			return nil, e
		}
		// No group found (or the list failed) sent the pool without
		// securityGroupId — the spec takes that — while the description
		// promises a zone-matched SSH-access group; provision refuses the
		// same case (final audit, stage 2).
		sgs, se := eng.Cloud.ListSecurityGroups(ctx, pid)
		if se != nil {
			return nil, fmt.Errorf(i18n.T("list security groups: %w (or pass security_group_id)", "список групп безопасности: %w (или передайте security_group_id)"), se)
		}
		i := provision.PickSecurityGroup(sgs, zone.computeID, zone.name)
		if i < 0 {
			return nil, clierrors.New(clierrors.CodeConfigInvalid,
				i18n.Tf("no security group of the project in zone %s for the node pool; nothing was created", "в зоне %s нет группы безопасности проекта для группы узлов; ничего не создано", zone.name),
				i18n.T("Pass security_group_id (list_security_groups), or create a group in this zone in the Cloud.ru console.", "Передайте security_group_id (list_security_groups) или создайте группу в этой зоне в консоли Cloud.ru."))
		}
		sgID, sgName = sgs[i].ID, sgs[i].Name
	}

	// The number of nodes and the size of their disks are billed: the
	// preview names them, defaults included (FB15-41).
	count := intArg(args, "count", 2)
	diskSize := intArg(args, "disk_gb", 20)
	// The preview is built for the question only: its price is a request
	// a consented call does not make.
	preview := func(ctx context.Context) (map[string]any, error) {
		preview, err := s.completePreview(ctx, nil, k8sPreview(engine.ResourceK8sNodePool, name, args,
			map[string]any{"flavor": flavor.Name, "cpu": flavor.CPU, "ram_gb": flavor.RAM, "count": count, "disk_gb": diskSize,
				"zone": zone.name, "nodes_subnet_id": nodesSubnetID, "nodes_subnet_name": nodesSubnetName,
				"security_group_id": sgID, "security_group_name": sgName}))
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"message": i18n.T("k8s_add_nodepool creates billable worker nodes.",
				"k8s_add_nodepool создаёт платные рабочие узлы."),
			"preview": preview,
		}, nil
	}
	return &Plan{PreviewOf: preview, Execute: func(ctx context.Context) (string, bool) {
		req := mk8s.CreateNodePoolReq{
			ClusterID:            clusterID,
			Name:                 name,
			ScalePolicy:          mk8s.ScalePolicy{FixedScale: &mk8s.FixedScale{Count: count}},
			MachineConfiguration: mk8s.MachineConfigurationRequest{FlavorID: flavor.FlavorID, Disk: &mk8s.DiskRequest{Size: diskSize}},
			NetworkConfiguration: mk8s.NodePoolNetworkConfigurationRequest{NodesSubnetID: nodesSubnetID, SecurityGroupID: sgID},
			UpdateConfiguration:  mk8s.NodePoolUpdateConfiguration{Strategy: "NODE_POOL_UPDATE_STRATEGY_ROLLING_UPDATE"},
		}
		op, err := cli.CreateNodePool(ctx, req)
		if err != nil || op.ID == "" {
			if err == nil {
				err = noID("operation")
			}
			return lostCreate(ctx, "k8s_add_nodepool", list, i18n.T("node pool", "группа узлов"), name, nil, "k8s_nodepools cluster_id="+clusterID, fmt.Errorf(i18n.T("create node pool: %w", "создание группы узлов: %w"), err))
		}
		return toJSON(map[string]any{
			"status": "creating", "node_pool": name, "nodepool_id": op.ResourceID, "cluster_id": clusterID,
			"operation_id": op.ID, "count": count, "flavor_id": flavor.FlavorID, "flavor": flavor.Name,
			"note": i18n.T("Node pool is provisioning (async). Poll k8s_nodepools. NB: if Cloud.ru returns 503 'no healthy upstream' it's a transient backend outage — retry.",
				fmt.Sprintf("Группа узлов создаётся (асинхронно). Проверяйте evo k8s nodepools %s. Если Cloud.ru отвечает 503 «no healthy upstream», это временный сбой сервиса — повторите.", clusterID)),
		}), false
	}}, nil
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
				i18n.Tf("master_count must be 1, 3 or 5, got %v", "master_count должен быть 1, 3 или 5, передано %v", args["master_count"]),
				i18n.T("1 — a zonal control plane; 3 or 5 — a highly available one.", "1 — зональные мастер-узлы; 3 или 5 — отказоустойчивые."))
		}
	}
	// The name the cluster gets, by the rule of the mk8s specification
	// (QA-15): the API refused it only after the plan.
	return k8sClusterName(k8sClusterNameArg(args))
}

// k8sClusterNameArg is the name a new cluster gets: the argument, else
// k8s-<product>.
func k8sClusterNameArg(args map[string]any) string {
	return firstNonEmptyStr(stringArg(args, "name"), "k8s-"+firstNonEmptyStr(stringArg(args, "product"), "cluster"))
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
		return k8sZoneRef{}, clierrors.New(clierrors.CodeUsage, i18n.T("zone is required: productConfiguration names no default zone", "нужна zone: в productConfiguration нет зоны по умолчанию"),
			i18n.T("Pass zone, e.g. ru.AZ-2 (k8s_options lists the zones).", "Передайте --zone, например ru.AZ-2 (зоны — в выводе evo k8s options)."))
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return k8sZoneRef{}, err
	}
	czones, err := eng.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return k8sZoneRef{}, fmt.Errorf(i18n.T("list availability zones: %w", "список зон доступности: %w"), err)
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
	// The Russian hint is the CLI's (MCP speaks English, D6): evo's
	// commands, not the tools (Cursor review of 1.2.2, CUR122-5).
	hint := i18n.T("k8s_options lists the zones of Managed Kubernetes.", "Зоны Managed Kubernetes — в выводе evo k8s options.")
	if len(offered) > 0 {
		hint = i18n.Tf("Zones of Managed Kubernetes: %s (k8s_options).", "Зоны Managed Kubernetes: %s (evo k8s options).", strings.Join(offered, ", "))
	}
	switch {
	case z.computeID == "" && z.mk8sID == "":
		return k8sZoneRef{}, clierrors.New(clierrors.CodeUsage, i18n.Tf("availability zone %q not found", "зона доступности %q не найдена", ref), hint)
	case z.mk8sID == "" && len(pc.Zones) > 0:
		return k8sZoneRef{}, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("zone %s is not offered for Managed Kubernetes", "зона %s недоступна для Managed Kubernetes", z.name), hint)
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
func (s *Registry) k8sVIPSubnet(ctx context.Context, pid, subnetID string, zone k8sZoneRef) (id, name string, err error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", "", err
	}
	subs, err := eng.Cloud.ListSubnets(ctx, pid)
	if err != nil {
		return "", "", fmt.Errorf(i18n.T("list subnets: %w", "список подсетей: %w"), err)
	}
	if subnetID != "" {
		for _, sn := range subs {
			if sn.ID != subnetID {
				continue
			}
			if !provision.SubnetInZone(sn, zone.computeID, zone.name) {
				return "", "", clierrors.New(clierrors.CodeConfigInvalid,
					i18n.Tf("subnet %s is not in zone %s, where the control plane goes; nothing was created", "подсеть %s не в зоне %s, где будут мастер-узлы; ничего не создано", subnetID, zone.name),
					i18n.T("Pass a subnet of that zone (list_subnets), or pass zone = the subnet's zone.", "Передайте подсеть этой зоны (list_subnets) или zone — зону подсети."))
			}
			return subnetID, sn.Name, nil
		}
		return "", "", clierrors.New(clierrors.CodeConfigInvalid, i18n.Tf("subnet %s is not in the project", "подсети %s нет в проекте", subnetID),
			i18n.T("Take a subnet ID from list_subnets, or omit subnet_id: the zone's main subnet is taken.", "Возьмите ID подсети из list_subnets или не указывайте subnet_id: возьмётся основная подсеть зоны."))
	}
	if i := provision.PickSubnet(subs, zone.computeID, zone.name); i >= 0 {
		return subs[i].ID, subs[i].Name, nil
	}
	return "", "", clierrors.New(clierrors.CodeConfigInvalid,
		i18n.Tf("no subnet in zone %s for the control-plane VIP; nothing was created", "в зоне %s нет подсети для VIP мастер-узлов; ничего не создано", zone.name),
		i18n.T("Pass subnet_id (list: list_subnets) or another zone; ", "Передайте subnet_id (список: list_subnets) или другую зону; ")+engine.NoSubnetAnywhere())
}

// k8sVIPSubnet also gives the subnet's name as the list gives it (Линия
// CLI), read here, not again.

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
func (s *Registry) k8sNodesSubnet(ctx context.Context, pid, subnetID string, zone k8sZoneRef) (id, name string, err error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", "", err
	}
	subs, err := eng.Cloud.ListSubnets(ctx, pid)
	if err != nil {
		return "", "", fmt.Errorf(i18n.T("list subnets: %w", "список подсетей: %w"), err)
	}
	if subnetID != "" {
		for _, sn := range subs {
			if sn.ID == subnetID {
				if !provision.SubnetInZone(sn, zone.computeID, zone.name) {
					return "", "", clierrors.New(clierrors.CodeConfigInvalid,
						i18n.Tf("subnet %s is not in zone %s of the node pool; nothing was created", "подсеть %s не в зоне %s группы узлов; ничего не создано", subnetID, zone.name),
						i18n.T("Pass a subnet of that zone (list_subnets), or pass zone = the subnet's zone.", "Передайте подсеть этой зоны (list_subnets) или zone — зону подсети."))
				}
				return subnetID, sn.Name, nil
			}
		}
		return "", "", clierrors.New(clierrors.CodeConfigInvalid, i18n.Tf("subnet %s is not in the project", "подсети %s нет в проекте", subnetID),
			i18n.T("Take a subnet ID from list_subnets, or omit nodes_subnet_id: the zone's main subnet is taken.", "Возьмите ID подсети из list_subnets или не указывайте nodes_subnet_id: возьмётся основная подсеть зоны."))
	}
	if i := provision.PickSubnet(subs, zone.computeID, zone.name); i >= 0 {
		return subs[i].ID, subs[i].Name, nil
	}
	return "", "", clierrors.New(clierrors.CodeConfigInvalid,
		i18n.Tf("no subnet in zone %s for the node pool; nothing was created", "в зоне %s нет подсети для группы узлов; ничего не создано", zone.name),
		i18n.T("Pass nodes_subnet_id (list: list_subnets) or another zone; ", "Передайте nodes_subnet_id (список: list_subnets) или другую зону; ")+engine.NoSubnetAnywhere())
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// k8sPreview is the preview of k8s_create and k8s_add_nodepool: what the
// call settled before the consent in place of the arguments of the same
// name, and the other arguments as given. resolved holds flavor, cpu,
// ram_gb and zone; for a cluster also version, master_count and subnet_id
// (k8sPlan.resolved), for a node pool nodes_subnet_id and
// security_group_id (D29, A12). evo has no rate card for Managed Kubernetes: the preview links
// its tariff (Price).
func k8sPreview(resource, name string, args, resolved map[string]any) engine.CreatePreview {
	p := engine.CreatePreview{Resource: resource, Name: name}
	z, _ := resolved["zone"].(string)
	if z == "" {
		z = stringArg(args, "zone")
	}
	if z != "" {
		if looksUUID(z) {
			p.Zone = &deployplan.RefSpec{ID: z}
		} else {
			p.Zone = &deployplan.RefSpec{Name: z}
		}
	}
	p.Add("cluster_id", stringArg(args, "cluster_id"))
	skip := []string{"name", "zone", "cluster_id"}
	for _, k := range []string{"flavor", "cpu", "ram_gb", "count", "disk_gb", "version", "master_count", "subnet_id", "subnet_name", "nodes_subnet_id", "nodes_subnet_name", "security_group_id", "security_group_name", "kube_api_internet"} {
		if v, ok := resolved[k]; ok {
			p.Add(k, v)
			skip = append(skip, k)
		}
	}
	p.AddArgs(args, skip...)
	p.Price()
	return p
}
