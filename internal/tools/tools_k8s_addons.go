// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
)

// Addons of Managed Kubernetes: k8s_addons (the catalogue), k8s_addon_versions
// (the releases of one addon) and k8s_add_addon (install one into a
// cluster). The catalogue is read-only, but the API reads it per project
// (projectId is required: without a project these tools answer
// CONFIG_INVALID before any request); installing was
// live-verified with cert-manager (docs/internal/standalone/BACKLOG.md
// [k8s-addons]).

// addonRow is one compact row of k8s_addons.
type addonRow struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name,omitempty"`
	Categories  []string       `json:"categories"`
	Billable    bool           `json:"billable"`
	DeployWith  map[string]any `json:"deploy_with"`
}

// handleK8sAddons searches the addon catalogue. Read-only.
func (s *Registry) handleK8sAddons(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	all, err := cli.ListAddons(ctx)
	if err != nil {
		return errResult(err), true
	}
	query := strings.ToLower(strings.TrimSpace(stringArg(args, "query")))
	category := strings.TrimSpace(stringArg(args, "category"))
	billable, filterBillable := args["billable"].(bool)

	cats := map[string]bool{}
	rows := []addonRow{}
	for _, a := range all {
		for _, c := range a.Categories {
			cats[c] = true
		}
		if query != "" && !strings.Contains(strings.ToLower(a.Name+"\n"+a.DisplayName+"\n"+a.Description), query) {
			continue
		}
		if category != "" && !hasCategory(a, category) {
			continue
		}
		if filterBillable && a.IsBillable != billable {
			continue
		}
		categories := a.Categories
		if categories == nil {
			categories = []string{}
		}
		rows = append(rows, addonRow{
			Name: a.Name, DisplayName: a.DisplayName, Categories: categories, Billable: a.IsBillable,
			DeployWith: map[string]any{"tool": "k8s_add_addon", "args": map[string]any{"name": a.Name}},
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	catList := make([]string, 0, len(cats))
	for c := range cats {
		catList = append(catList, c)
	}
	sort.Strings(catList)
	return toJSON(map[string]any{"addons": rows, "shown": len(rows), "total": len(all), "categories": catList}), false
}

func hasCategory(a mk8s.Addon, category string) bool {
	for _, c := range a.Categories {
		if strings.EqualFold(c, category) {
			return true
		}
	}
	return false
}

// findAddon is the catalogue entry with this name (case-insensitive), or
// NOT_FOUND with the command that searches the catalogue.
func findAddon(ctx context.Context, cli *mk8s.Client, name string) (mk8s.Addon, error) {
	all, err := cli.ListAddons(ctx)
	if err != nil {
		return mk8s.Addon{}, err
	}
	for _, a := range all {
		if strings.EqualFold(a.Name, name) {
			return a, nil
		}
	}
	hint := i18n.T("List the addons: evo k8s addons <substring> (MCP: k8s_addons query=…).",
		"Список плагинов: evo k8s addons <подстрока> (MCP: k8s_addons query=…).")
	if similar := similarAddons(all, name); len(similar) > 0 {
		hint += i18n.Tf(" Similar: %s.", " Похожие: %s.", strings.Join(similar, ", "))
	}
	return mk8s.Addon{}, clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("addon %q not found in the Managed Kubernetes catalogue", "плагин %q не найден в каталоге Managed Kubernetes", name), hint)
}

// similarAddons are up to five catalogue names that contain the first word
// of a name that found nothing ("cert" of "cert-manger").
func similarAddons(all []mk8s.Addon, name string) []string {
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == '.' })
	if len(words) == 0 || len([]rune(words[0])) < 3 {
		return nil
	}
	word := words[0]
	var out []string
	for _, a := range all {
		if strings.Contains(strings.ToLower(a.Name+" "+a.DisplayName), word) {
			out = append(out, a.Name)
		}
	}
	sort.Strings(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// releaseRow is one release of k8s_addon_versions.
func releaseRow(r mk8s.AddonRelease, latest, full bool) map[string]any {
	row := map[string]any{
		"version":       r.AppVersion,
		"addon_version": r.AddonVersion,
		"mode":          mk8s.NormalizeMode(r.Mode),
		"kube_versions": r.Requirements.KubeVersions,
		"ready_workers": int(r.Requirements.ReadyWorkers),
		"namespace":     r.AddonNamespace,
		"has_schema":    r.ConfigurationSchema != "",
		"schema_bytes":  len(r.ConfigurationSchema),
		"latest":        latest,
	}
	if full && r.ConfigurationSchema != "" {
		// The schema as JSON when it is JSON, else the string as is.
		if json.Valid([]byte(r.ConfigurationSchema)) {
			row["configuration_schema"] = json.RawMessage(r.ConfigurationSchema)
		} else {
			row["configuration_schema"] = r.ConfigurationSchema
		}
	}
	return row
}

// handleK8sAddonVersions lists the releases of an addon, newest first.
// Read-only. The configuration schema (up to ~94 KB) only with full=true.
func (s *Registry) handleK8sAddonVersions(ctx context.Context, args map[string]any) (string, bool) {
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		return usageT("name is required (an addon name from k8s_addons)", "нужен name — имя плагина из k8s_addons"), true
	}
	full := boolArg(args, "full", false)
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	addon, err := findAddon(ctx, cli, name)
	if err != nil {
		return errResult(err), true
	}
	rels, err := cli.GetAddonReleases(ctx, addon.Name)
	if err != nil {
		return errResult(err), true
	}
	mk8s.SortReleases(rels)
	latest, hasLatest := mk8s.LatestRelease(rels)
	rows := make([]map[string]any, 0, len(rels))
	for _, r := range rels {
		isLatest := hasLatest && r.AppVersion == latest.AppVersion && r.AddonVersion == latest.AddonVersion
		rows = append(rows, releaseRow(r, isLatest, full))
	}
	out := map[string]any{
		"addon":        addon.Name,
		"display_name": addon.DisplayName,
		"billable":     addon.IsBillable,
		"releases":     rows,
	}
	if hasLatest {
		out["latest"] = latest.AppVersion
		out["deploy_with"] = map[string]any{"tool": "k8s_add_addon",
			"args": map[string]any{"name": addon.Name, "version": latest.AppVersion}}
	} else {
		out["note"] = "No installable release (all END_OF_LIFE or INACTIVE)."
	}
	return toJSON(out), false
}

// addonConfig reads the config argument: a JSON object, or a string that
// holds one. It returns the JSON text sent as addonConfiguration and the
// top-level keys for the preview.
func addonConfig(v any) (string, []string, error) {
	var obj map[string]any
	switch x := v.(type) {
	case nil:
		return "", nil, nil
	case map[string]any:
		obj = x
	case string:
		if strings.TrimSpace(x) == "" {
			return "", nil, nil
		}
		if err := json.Unmarshal([]byte(x), &obj); err != nil || obj == nil {
			return "", nil, fmt.Errorf(i18n.T("config must be a JSON object (or a string holding one): %v", "config должен быть объектом JSON (или строкой с ним): %v"), firstErr(err, i18n.T("not an object", "не объект")))
		}
	default:
		return "", nil, fmt.Errorf(i18n.T("config must be a JSON object (or a string holding one), got %T", "config должен быть объектом JSON (или строкой с ним), передано %T"), v)
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return "", nil, fmt.Errorf("config: %w", err)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return string(b), keys, nil
}

func firstErr(err error, def string) string {
	if err != nil {
		return err.Error()
	}
	return def
}

// addonPlan is what k8s_add_addon is about to install and what it found
// out about the cluster.
type addonPlan struct {
	addon       mk8s.Addon
	release     mk8s.AddonRelease
	requested   bool // the version was given, not picked
	clusterName string
	kubeVersion string // "" when the cluster answer has none
	workersMin  int    // node pools' desired nodes (autoscale: min)
	workersMax  int    // autoscale: max
	workersRead bool
	installed   *bool
	notChecked  []string
	warnings    []string
	blockers    []string // requirements that certainly fail
}

// clusterKubeVersion finds the Kubernetes version in a cluster answer. The
// live shape of GET /v3/clusters/{id} is not recorded, so the likely places
// are tried; a value that is not a version is skipped.
func clusterKubeVersion(m map[string]any) string {
	keys := []string{"version", "kubernetesVersion", "kubeVersion", "currentVersion"}
	pick := func(obj map[string]any) string {
		for _, k := range keys {
			// A string that is not a version compares below 0.0.0.
			if s, ok := obj[k].(string); ok && mk8s.CompareVersions(s, "0.0.0") >= 0 {
				return s
			}
		}
		return ""
	}
	if v := pick(m); v != "" {
		return v
	}
	for _, nest := range []string{"controlPlane", "controlPlaneConfiguration", "spec", "status"} {
		if obj, ok := m[nest].(map[string]any); ok {
			if v := pick(obj); v != "" {
				return v
			}
		}
	}
	return ""
}

// numAny reads a count that protojson may write as a string.
func numAny(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n, true
		}
	}
	return 0, false
}

// poolNodes is the node count a pool asks for: fixed, or autoscale min-max.
func poolNodes(np map[string]any) (minN, maxN int, ok bool) {
	sp, _ := np["scalePolicy"].(map[string]any)
	if fs, isFixed := sp["fixedScale"].(map[string]any); isFixed {
		n, ok := numAny(fs["count"])
		return n, n, ok
	}
	if as, isAuto := sp["autoScale"].(map[string]any); isAuto {
		lo, ok1 := numAny(as["minCount"])
		hi, ok2 := numAny(as["maxCount"])
		return lo, hi, ok1 && ok2
	}
	return 0, 0, false
}

// planAddon resolves the release and checks the cluster: only reads.
func planAddon(ctx context.Context, cli *mk8s.Client, clusterID, name, version string) (*addonPlan, error) {
	addon, err := findAddon(ctx, cli, name)
	if err != nil {
		return nil, err
	}
	rels, err := cli.GetAddonReleases(ctx, addon.Name)
	if err != nil {
		return nil, err
	}
	// Newest first: with several releases of one appVersion FindRelease
	// takes the newest addonVersion, not whichever the API listed first.
	mk8s.SortReleases(rels)
	p := &addonPlan{addon: addon}
	if version != "" {
		rel, ok := mk8s.FindRelease(rels, version)
		if !ok {
			return nil, clierrors.New(clierrors.CodeNotFound,
				i18n.Tf("addon %s has no version %q", "у плагина %s нет версии %q", addon.Name, version),
				i18n.Tf("Versions: evo k8s addon-versions %s (MCP: k8s_addon_versions).", "Версии: evo k8s addon-versions %s (MCP: k8s_addon_versions).", addon.Name))
		}
		// POST /v3/clusterAddons takes appVersion only (openapi-v3,
		// CreateClusterAddonRequest): the server picks the addonVersion. A
		// release asked for by an addonVersion other than the one the
		// server is expected to pick for its appVersion (the newest — an
		// assumption) cannot be pinned.
		if same := releasesOf(rels, rel.AppVersion); len(same) > 1 {
			if rel.AddonVersion != same[0].AddonVersion {
				return nil, clierrors.New(clierrors.CodeUsage,
					i18n.Tf("addon version %s cannot be pinned: the API installs by app version (%s) and picks the addon version itself",
						"версию плагина %s закрепить нельзя: API ставит по версии приложения (%s) и сам выбирает версию плагина", rel.AddonVersion, rel.AppVersion),
					i18n.Tf("Give the app version: --version %s.", "Укажите версию приложения: --version %s.", rel.AppVersion))
			}
			p.warnings = append(p.warnings, fmt.Sprintf("app version %s has %d addon versions; the server picks one (expected %s)", rel.AppVersion, len(same), same[0].AddonVersion))
		}
		p.release, p.requested = rel, true
		switch m := mk8s.NormalizeMode(rel.Mode); m {
		case mk8s.ModeEndOfLife, mk8s.ModeInactive, mk8s.ModeEndOfSupport:
			p.warnings = append(p.warnings, fmt.Sprintf("version %s is %s", rel.AppVersion, m))
		}
	} else {
		rel, ok := mk8s.LatestRelease(rels)
		if !ok {
			return nil, clierrors.New(clierrors.CodeNotFound,
				i18n.Tf("addon %s has no installable version", "у плагина %s нет версии для установки", addon.Name),
				i18n.Tf("Versions: evo k8s addon-versions %s.", "Версии: evo k8s addon-versions %s.", addon.Name))
		}
		p.release = rel
		if m := mk8s.NormalizeMode(rel.Mode); m != mk8s.ModeActive {
			p.warnings = append(p.warnings, fmt.Sprintf("no ACTIVE release; picked %s (%s)", rel.AppVersion, firstNonEmptyStr(m, "no mode")))
		}
	}

	// The cluster must exist: its error is the call's error.
	cl, err := cli.GetCluster(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	p.clusterName, _ = cl["name"].(string)
	p.kubeVersion = clusterKubeVersion(cl)
	req := p.release.Requirements
	if req.KubeVersions != "" {
		ok, known := mk8s.CheckKubeVersion(req.KubeVersions, p.kubeVersion)
		switch {
		case !known:
			p.notChecked = append(p.notChecked, "kube_version")
		case !ok:
			p.blockers = append(p.blockers, i18n.Tf("the cluster runs Kubernetes %s, %s %s needs %s",
				"в кластере Kubernetes %s, а %s %s нужен %s", p.kubeVersion, addon.Name, p.release.AppVersion, req.KubeVersions))
		}
	}

	// Ready workers: the node pools' desired counts are an upper bound of
	// the ready nodes, so only a shortfall is certain.
	if pools, err := cli.ListNodePools(ctx, clusterID); err == nil {
		p.workersRead = true
		for _, np := range pools {
			if cid, ok := np["clusterId"].(string); ok && cid != "" && cid != clusterID {
				continue
			}
			lo, hi, ok := poolNodes(np)
			if !ok {
				p.workersRead = false
				continue
			}
			p.workersMin += lo
			p.workersMax += hi
		}
	}
	if need := int(req.ReadyWorkers); need > 0 {
		switch {
		case !p.workersRead:
			p.notChecked = append(p.notChecked, "ready_workers")
		case p.workersMax < need:
			p.blockers = append(p.blockers, i18n.Tf("%s %s needs %d ready worker node(s), the cluster's node pools have %d",
				"%s %s нужно готовых рабочих узлов: %d, а в группах узлов кластера их %d", addon.Name, p.release.AppVersion, need, p.workersMax))
		case p.workersMin < need:
			p.notChecked = append(p.notChecked, "ready_workers")
		default:
			p.warnings = append(p.warnings, fmt.Sprintf("node pools ask for %d node(s); whether %d are ready is not checked", p.workersMin, need))
		}
	}

	// Already installed: informational, and the list call is not
	// live-verified, so an error is "unknown".
	if list, err := cli.ListClusterAddons(ctx, clusterID); err == nil {
		inst := false
		for _, ca := range list {
			for _, k := range []string{"name", "addonName"} {
				if n, _ := ca[k].(string); strings.EqualFold(n, addon.Name) {
					inst = true
				}
			}
		}
		p.installed = &inst
		if inst {
			p.warnings = append(p.warnings, addon.Name+" is already installed in this cluster: the API may refuse, or resolve_mode decides what happens to it")
		}
	} else {
		p.notChecked = append(p.notChecked, "installed")
	}
	return p, nil
}

// preview is the CreatePreview of the plan, in the lines of the other
// k8s previews.
func (p *addonPlan) preview(clusterID, resolveMode string, configKeys []string) engine.CreatePreview {
	cp := engine.CreatePreview{Resource: engine.ResourceK8sAddon, Name: p.addon.Name}
	r := p.release
	cp.Add("cluster_id", clusterID)
	cp.Add("cluster_name", p.clusterName)
	cp.Add("version", r.AppVersion)
	cp.Add("addon_version", r.AddonVersion)
	if m := mk8s.NormalizeMode(r.Mode); m != mk8s.ModeActive {
		cp.Add("support", m)
	}
	cp.Add("namespace", r.AddonNamespace)
	cp.Add("requires_kube", r.Requirements.KubeVersions)
	cp.Add("cluster_version", p.kubeVersion)
	if n := int(r.Requirements.ReadyWorkers); n > 0 {
		cp.Add("requires_workers", n)
	}
	if p.workersRead {
		if p.workersMin == p.workersMax {
			cp.Add("worker_nodes", p.workersMin)
		} else {
			cp.Add("worker_nodes", fmt.Sprintf("%d-%d", p.workersMin, p.workersMax))
		}
	}
	if p.installed != nil {
		cp.Add("installed", *p.installed)
	}
	cp.Add("billable", p.addon.IsBillable)
	// As a person writes it (NONE, OVERWRITE): the request carries the API
	// value RESOLVE_CONFLICTS_MODE_*.
	cp.Add("resolve_mode", strings.TrimPrefix(resolveMode, "RESOLVE_CONFLICTS_MODE_"))
	if len(configKeys) > 0 {
		cp.Add("config_keys", strings.Join(configKeys, ","))
	}
	if len(p.notChecked) > 0 {
		cp.Add("not_checked", p.notChecked)
	}
	cp.Price()
	return cp
}

// handleK8sAddAddon installs an addon release into a cluster (gated).
// Without confirmed it only reads: the catalogue, the cluster, its node
// pools and addons, to show the version it picked and the requirements.
func (s *Registry) handleK8sAddAddon(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := strings.TrimSpace(stringArg(args, "cluster_id"))
	name := strings.TrimSpace(stringArg(args, "name"))
	if clusterID == "" || name == "" {
		return usageT("cluster_id and name are required", "нужны cluster_id и name"), true
	}
	config, configKeys, err := addonConfig(args["config"])
	if err != nil {
		return usageResult(err), true
	}
	// Always sent: the API requires it (default NONE).
	resolveMode, err := mk8s.ResolveModeValue(stringArg(args, "resolve_mode"))
	if err != nil {
		return usageResult(err), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	plan, err := planAddon(ctx, cli, clusterID, name, strings.TrimSpace(stringArg(args, "version")))
	if err != nil {
		return errResult(err), true
	}
	r := plan.release
	if len(plan.blockers) > 0 {
		return errResult(clierrors.New(clierrors.CodeUsage, strings.Join(plan.blockers, "; "),
			i18n.Tf("Versions and their requirements: evo k8s addon-versions %s; worker nodes: evo k8s add-nodepool.",
				"Версии и их требования: evo k8s addon-versions %s; рабочие узлы: evo k8s add-nodepool.", plan.addon.Name))), true
	}
	requirements := map[string]any{"kube_versions": r.Requirements.KubeVersions, "ready_workers": int(r.Requirements.ReadyWorkers)}
	versionFrom := "latest"
	if plan.requested {
		versionFrom = "requested"
	}

	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		preview, err := s.completePreview(ctx, nil, plan.preview(clusterID, resolveMode, configKeys))
		if err != nil {
			return errResult(err), true
		}
		out := map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("k8s_add_addon installs %s %s into cluster %s (%s).",
				"k8s_add_addon устанавливает %s %s в кластер %s (%s).",
				plan.addon.Name, r.AppVersion, clusterID, addonNamespaceText(r.AddonNamespace)),
			"preview":      preview,
			"version_from": versionFrom,
			"requirements": requirements,
		}
		if len(plan.notChecked) > 0 {
			out["not_checked"] = plan.notChecked
		}
		if len(plan.warnings) > 0 {
			out["warnings"] = plan.warnings
		}
		return toJSON(out), true
	}

	res, err := cli.CreateClusterAddon(ctx, mk8s.CreateClusterAddonReq{
		ClusterID: clusterID, Name: plan.addon.Name, AppVersion: r.AppVersion,
		AddonConfiguration: config, ResolveMode: resolveMode,
	})
	if err != nil {
		// A failed operation still names itself: the agent can look it up.
		if res != nil && res.OperationID != "" {
			return errAnswer(err, map[string]any{"operation_id": res.OperationID}), true
		}
		return errResult(err), true
	}
	check := "get pods -A"
	if r.AddonNamespace != "" {
		check = "get pods -n " + r.AddonNamespace
	}
	out := map[string]any{
		"status": "installing", "cluster_id": clusterID, "addon": plan.addon.Name,
		"version": r.AppVersion, "requirements": requirements,
		"note": "Installation is async. Next: check the addon's pods with k8s_kubectl args=\"" + check + "\" (CLI: evo k8s kubectl " + clusterID + " " + check + ") " +
			"until they are Running and Ready=True; pods of a Job (e.g. cert-manager-startupapicheck) end Completed. " +
			"Pods stuck in ImagePullBackOff may mean the cluster has no internet egress: Cloud.ru needs a public SNAT gateway in the nodes' zone (" + snatDocURL + "). " +
			"evo has no command to remove an addon: remove it in the Cloud.ru console; it also goes with the cluster (k8s_delete).",
	}
	if r.AddonNamespace != "" {
		out["namespace"] = r.AddonNamespace
	}
	if res.OperationID != "" {
		out["operation_id"] = res.OperationID
	}
	if res.ID != "" {
		out["cluster_addon_id"] = res.ID
	}
	if res.Status != "" {
		out["addon_status"] = res.Status
	}
	if len(plan.warnings) > 0 {
		out["warnings"] = plan.warnings
	}
	return toJSON(out), false
}

// snatDocURL is the Cloud.ru page on Managed Kubernetes limitations: a
// cluster and its pods reach the internet through a public SNAT gateway in
// the nodes' zone.
const snatDocURL = "https://cloud.ru/docs/kubernetes-evolution/ug/topics/overview__limitations"

// addonNamespaceText names the namespace an addon installs into. The
// release's addonNamespace is not reliable (the live API repeats the addon
// version in it, see mk8s.GetAddonReleases): without a usable one it is
// "the addon's default namespace", not a guess.
func addonNamespaceText(ns string) string {
	if ns == "" {
		return i18n.T("the addon's default namespace", "пространство имён плагина по умолчанию")
	}
	return i18n.T("namespace ", "пространство имён ") + ns
}

// releasesOf is the releases of one appVersion, in the order of rels
// (newest addonVersion first after SortReleases).
func releasesOf(rels []mk8s.AddonRelease, appVersion string) []mk8s.AddonRelease {
	var out []mk8s.AddonRelease
	for _, r := range rels {
		if r.AppVersion == appVersion {
			out = append(out, r)
		}
	}
	return out
}
