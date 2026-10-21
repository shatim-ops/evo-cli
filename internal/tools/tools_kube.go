// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/kubetools"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
)

// kubeconfigForCluster fetches a cluster's kubeconfig with an embedded bearer
// token (no cloudlogin needed), ready to hand to kubectl/helm.
func (s *Registry) kubeconfigForCluster(ctx context.Context, clusterID string) (string, error) {
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return "", err
	}
	cfg, err := cli.GetKubeConfig(ctx, clusterID, "")
	if err != nil {
		return "", err
	}
	plain := cfg
	if b, e := base64.StdEncoding.DecodeString(cfg); e == nil && len(b) > 0 {
		plain = string(b)
	}
	keyID, secret := s.accessKey()
	if keyID == "" || secret == "" {
		return "", errors.New(i18n.T("no Cloud.ru access key configured (evo configure, or CLOUDRU_KEY_ID/CLOUDRU_SECRET) — cannot mint a kube token",
			"не задан ключ доступа Cloud.ru (evo configure или CLOUDRU_KEY_ID/CLOUDRU_SECRET): токен для кластера не получить"))
	}
	tok, err := mk8s.FetchOIDCToken(ctx, mk8s.OIDCURLFromKubeconfig(plain), keyID, secret)
	if err != nil {
		return "", fmt.Errorf(i18n.T("kube token exchange: %w", "получение токена кластера: %w"), err)
	}
	return mk8s.EmbedToken(plain, tok)
}

// readOnlyKubectl is the allowlist of verbs that run freely; anything else
// (create/delete/patch/scale/rollout restart/exec/…) is a mutation and needs
// confirmed=true. Safe-by-default: an unknown verb is treated as mutating.
var readOnlyKubectl = map[string]bool{
	"get": true, "describe": true, "logs": true, "log": true, "top": true,
	"version": true, "api-resources": true, "api-versions": true, "explain": true,
	"events": true, "cluster-info": true, "config": true, "auth": true,
	"rollout": true, "diff": true, "wait": true, // rollout status/history are reads
}

// readOnlySubcommands narrows the verbs above whose subcommands may also
// write: "rollout restart|undo|pause|resume" and "auth reconcile" change the
// cluster, so only these subcommands of them run without confirmation.
var readOnlySubcommands = map[string]map[string]bool{
	"rollout": {"status": true, "history": true},
	"auth":    {"can-i": true, "whoami": true},
}

// kubectlHostFlags make even a read verb reach beyond the cluster, so a
// call with them needs confirmed=true like a mutation (security audit 19.10,
// SEC-4): -f/--filename and -k/--kustomize read a local path or fetch any
// URL ("get -f https://…" sends a request wherever the arguments say, "get
// -f /etc/…" prints the file in its error), --output-directory
// (cluster-info dump) writes files on the host. Names are as
// kubetools.ParseFlags gives them, so -f x, -fx, -f=x and --filename=x are
// all caught.
//
// Also --cache-dir (the discovery cache is written there), any flag whose
// name has "file" or ends in "-dir" (--profile, --profile-output: a
// profile written on the host), and an output format read from a host file
// (-o go-template-file=…, jsonpath-file=…, custom-columns-file=…, in any
// form: -o x, -ox, -o=x, --output=x, --output x) (release audit, REL-4).
var kubectlHostFlags = map[string]bool{
	"f": true, "filename": true, "k": true, "kustomize": true, "output-directory": true,
	"cache-dir": true,
}

// kubectlReadOnly reports whether kubectl args (verb first) only read.
func kubectlReadOnly(fields []string) bool {
	if len(fields) == 0 || !readOnlyKubectl[fields[0]] {
		return false
	}
	if kubectlReachesHost(fields[0], fields[1:]) {
		return false
	}
	subs, ok := readOnlySubcommands[fields[0]]
	if !ok {
		return true
	}
	return len(fields) > 1 && subs[fields[1]]
}

// kubectlNeedsConsent reports whether a k8s_kubectl call needs
// confirmed=true: its command is not a read (kubectlReadOnly).
func kubectlNeedsConsent(args map[string]any) bool {
	return !kubectlReadOnly(strings.Fields(stringArg(args, "args")))
}

// kubectlReachesHost reports whether the arguments of verb carry one of
// kubectlHostFlags. The words after "--" are not kubectl flags. "logs -f"
// is --follow (logs has no --filename), not a file.
func kubectlReachesHost(verb string, args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return false
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		for _, fl := range kubetools.ParseFlags(a) {
			if fl.Follows(verb) {
				continue
			}
			if kubectlHostFlags[fl.Name] || strings.Contains(fl.Name, "file") || strings.HasSuffix(fl.Name, "-dir") {
				return true
			}
			if fl.Name == "o" || fl.Name == "output" {
				val := fl.Val
				if !fl.HasVal && i+1 < len(args) {
					i++
					val = args[i]
				}
				if strings.Contains(strings.ToLower(val), "file") {
					return true
				}
			}
		}
	}
	return false
}

// handleK8sKubectl runs ANY kubectl command against a cluster (uses an
// auto-embedded token — no cloudlogin needed). Read verbs run freely; verbs
// that mutate the cluster require confirmed=true. This is the general escape
// hatch for whatever a product's deploy/operate needs beyond helm/apply
// (create secret/configmap, scale, rollout restart, label, exec, cordon, …).
func (s *Registry) handleK8sKubectl(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	if clusterID == "" {
		return usageT("cluster_id is required", "нужен cluster_id"), true
	}
	raw := stringArg(args, "args")
	if raw == "" {
		return usageT("'args' is required, e.g. \"get pods -A\"", "нужен 'args', например \"get pods -A\""), true
	}
	fields := strings.Fields(raw)
	verb := ""
	if len(fields) > 0 {
		verb = fields[0]
	}
	// Before the read-only check: "get pods --server=…" is a read verb
	// that would send the cluster's token to another server unasked.
	if err := kubetools.CheckConnectionFlags(fields); err != nil {
		return usageResult(err), true
	}
	if kubectlNeedsConsent(args) {
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			return toJSON(map[string]any{
				"status": "confirmation_required",
				"message": i18n.Tf("kubectl %q changes the cluster. (Read-only verbs run without confirmation.)",
					"kubectl %q меняет кластер. (Команды чтения выполняются без подтверждения.)", verb),
			}), true
		}
	}
	kc, err := s.kubeconfigForCluster(ctx, clusterID)
	if err != nil {
		return errResult(err), true
	}
	out, err := kubetools.Kubectl(ctx, kc, "", 90*time.Second, fields...)
	if err != nil {
		return errAnswer(err, map[string]any{"output": out}), true
	}
	return toJSON(map[string]any{"status": "ok", "output": out}), false
}

// handleK8sApply applies a manifest (YAML/JSON) to the cluster. Mutation → confirmed.
func (s *Registry) handleK8sApply(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	manifest := stringArg(args, "manifest")
	if clusterID == "" || manifest == "" {
		return usageT("cluster_id and manifest are required", "нужны cluster_id и manifest"), true
	}
	// namespace goes to kubectl as an argument: a value starting with '-'
	// would be read as another flag (as for k8s_helm).
	if strings.HasPrefix(stringArg(args, "namespace"), "-") {
		return usageT("'namespace' must not start with '-'", "'namespace' не может начинаться с '-'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.T("k8s_apply changes the cluster.",
				"k8s_apply меняет кластер."),
		}), true
	}
	kc, err := s.kubeconfigForCluster(ctx, clusterID)
	if err != nil {
		return errResult(err), true
	}
	kargs := []string{"apply", "-f", "-"}
	if ns := stringArg(args, "namespace"); ns != "" {
		kargs = append(kargs, "-n", ns)
	}
	out, err := kubetools.Kubectl(ctx, kc, manifest, 120*time.Second, kargs...)
	if err != nil {
		return errAnswer(err, map[string]any{"output": out}), true
	}
	return toJSON(map[string]any{"status": "applied", "output": out}), false
}

// helmMutatingActions are the k8s_helm actions that change the cluster.
var helmMutatingActions = map[string]bool{"install": true, "upgrade": true, "uninstall": true, "delete": true}

// helmNeedsConsent reports whether a k8s_helm call needs confirmed=true:
// its action changes the cluster.
func helmNeedsConsent(args map[string]any) bool {
	return helmMutatingActions[strings.ToLower(stringArg(args, "action"))]
}

// handleK8sHelm drives helm (install/upgrade/uninstall/list/status). Mutating
// actions (install/upgrade/uninstall) need confirmed=true.
func (s *Registry) handleK8sHelm(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	action := strings.ToLower(stringArg(args, "action"))
	if clusterID == "" || action == "" {
		return usageT("cluster_id and action are required (install|upgrade|uninstall|list|status)",
			"нужны cluster_id и action (install|upgrade|uninstall|list|status)"), true
	}
	// release and chart go to helm as positional arguments: a value starting
	// with '-' would be parsed as a flag (--post-renderer runs a program on
	// the server's host).
	for _, k := range []string{"release", "chart", "namespace", "version"} {
		if strings.HasPrefix(stringArg(args, k), "-") {
			return usageT("'%s' must not start with '-'", "'%s' не может начинаться с '-'", k), true
		}
	}
	// The action and its own arguments are checked before the confirmation
	// gate and the kubeconfig: an unknown action used to fetch the
	// kubeconfig (two POSTs to Cloud.ru) before it was refused (release
	// audit, A-024).
	release := stringArg(args, "release")
	chart := stringArg(args, "chart")
	switch action {
	case "install", "upgrade":
		if release == "" || chart == "" {
			return usageT("install/upgrade need release and chart", "для install и upgrade нужны release и chart"), true
		}
	case "uninstall", "delete":
		if release == "" {
			return usageT("uninstall needs release", "для uninstall нужен release"), true
		}
	case "status":
		if release == "" {
			return usageT("status needs release", "для status нужен release"), true
		}
	case "list":
	default:
		return usageT("unknown action %q (install|upgrade|uninstall|list|status)", "неизвестное действие %q (install|upgrade|uninstall|list|status)", action), true
	}
	// helm itself refused a relative repo_url ("non-absolute URLs"), and
	// that read as an API error (release audit, A-003).
	if raw := stringArg(args, "repo_url"); raw != "" {
		if u, err := url.Parse(raw); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "oci") {
			return usageT("'repo_url' must be an absolute https://, http:// or oci:// URL, got %q",
				"'repo_url' должен быть абсолютным URL https://, http:// или oci://, передано %q", raw), true
		}
	}
	if helmNeedsConsent(args) {
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			return toJSON(map[string]any{
				"status": "confirmation_required",
				"message": i18n.Tf("helm %s changes the cluster (billable workloads).",
					"helm %s меняет кластер (платные нагрузки).", action),
			}), true
		}
	}
	kc, err := s.kubeconfigForCluster(ctx, clusterID)
	if err != nil {
		return errResult(err), true
	}
	ns := stringArg(args, "namespace")

	// Optionally register a chart repo first (repo_url + a repo name derived
	// from the chart's "<repo>/<name>" prefix).
	if repoURL := stringArg(args, "repo_url"); repoURL != "" && (action == "install" || action == "upgrade") {
		repoName := "repo"
		if i := strings.Index(chart, "/"); i > 0 {
			repoName = chart[:i]
		}
		if out, e := kubetools.Helm(ctx, kc, 60*time.Second, "repo", "add", repoName, repoURL, "--force-update"); e != nil {
			return errAnswer(e, map[string]any{"stage": "repo add", "output": out}), true
		}
		if out, e := kubetools.Helm(ctx, kc, 90*time.Second, "repo", "update"); e != nil {
			return errAnswer(e, map[string]any{"stage": "repo update", "output": out}), true
		}
	}

	var hargs []string
	switch action {
	case "install", "upgrade":
		hargs = []string{"upgrade", "--install", release, chart}
		if ns != "" {
			hargs = append(hargs, "-n", ns, "--create-namespace")
		}
		if v := stringArg(args, "version"); v != "" {
			hargs = append(hargs, "--version", v)
		}
		// Inline values YAML → temp file passed as -f.
		if values := stringArg(args, "values"); values != "" {
			// A failed write must not install the chart with its defaults.
			vf, e := os.CreateTemp("", "helm-values-*.yaml")
			if e != nil {
				return errResult(fmt.Errorf("values file: %w", e)), true
			}
			defer os.Remove(vf.Name())
			_, e = vf.WriteString(values)
			if ce := vf.Close(); e == nil {
				e = ce
			}
			if e != nil {
				return errResult(fmt.Errorf("values file: %w", e)), true
			}
			hargs = append(hargs, "-f", vf.Name())
		}
		// --set overrides (comma-safe: passed as repeated --set).
		if sets, ok := args["set"].([]any); ok {
			for _, kv := range sets {
				if s, _ := kv.(string); s != "" {
					hargs = append(hargs, "--set", s)
				}
			}
		}
		hargs = append(hargs, "--wait", "--timeout", "8m")
	case "uninstall", "delete":
		hargs = []string{"uninstall", release}
		if ns != "" {
			hargs = append(hargs, "-n", ns)
		}
	case "list":
		hargs = []string{"list", "-A"}
	case "status":
		hargs = []string{"status", release}
		if ns != "" {
			hargs = append(hargs, "-n", ns)
		}
	}

	timeout := 120 * time.Second
	if action == "install" || action == "upgrade" {
		timeout = 9 * time.Minute // --wait can take a while pulling images
	}
	out, err := kubetools.Helm(ctx, kc, timeout, hargs...)
	if err != nil {
		return errAnswer(err, map[string]any{"output": out}), true
	}
	return toJSON(map[string]any{"status": "ok", "action": action, "output": out}), false
}
