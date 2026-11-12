// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
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
func (s *Registry) planK8sKubectl(_ context.Context, args map[string]any) (*Plan, error) {
	clusterID := stringArg(args, "cluster_id")
	if clusterID == "" {
		return answered(usageT("cluster_id is required", "нужен cluster_id"), true), nil
	}
	raw := stringArg(args, "args")
	if raw == "" {
		return answered(usageT("'args' is required, e.g. \"get pods -A\"", "нужен 'args', например \"get pods -A\""), true), nil
	}
	fields := strings.Fields(raw)
	verb := ""
	if len(fields) > 0 {
		verb = fields[0]
	}
	// Before the read-only check: "get pods --server=…" is a read verb
	// that would send the cluster's token to another server unasked.
	if err := kubetools.CheckConnectionFlags(fields); err != nil {
		return answered(usageResult(err), true), nil
	}
	preview := map[string]any{
		"message": i18n.Tf("kubectl %q changes the cluster. (Read-only verbs run without confirmation.)",
			"kubectl %q меняет кластер. (Команды чтения выполняются без подтверждения.)", verb),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		kc, err := s.kubeconfigForCluster(ctx, clusterID)
		if err != nil {
			return errResult(err), true
		}
		timeout, capped := callBound(ctx, 90*time.Second)
		out, err := kubetools.Kubectl(ctx, kc, "", timeout, fields...)
		if capped && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return toJSON(kubectlAtLimit(out, verb, args)), false
		}
		if err != nil {
			return errAnswer(err, map[string]any{"output": out}), true
		}
		return toJSON(map[string]any{"status": "ok", "output": out}), false
	}}, nil
}

// kubectlAtLimit is the answer of kubectl stopped at the limit of an MCP
// call (D69): not an error — what it printed so far, and what holds. What
// it sent the cluster before (an apply, a delete) goes on there; a command
// that waits (rollout status, wait, logs -f) is repeated, a change is
// checked with a read.
func kubectlAtLimit(out, verb string, args map[string]any) map[string]any {
	answer := map[string]any{
		"status": "stopped_at_limit",
		"output": out,
	}
	if kubectlReadOnly(strings.Fields(stringArg(args, "args"))) {
		answer["next"] = fmt.Sprintf("kubectl %s was stopped at the time limit of an MCP call; it reads and changes nothing. Call k8s_kubectl again: a command that waits (rollout status, wait, logs -f) goes on from the cluster's state; give it --timeout under 40s to answer within one call.", verb)
		// A read needs no consent: the step carries none («Риск»,
		// conclusion 126).
		nextSameCall(answer, "k8s_kubectl", args)
		return answer
	}
	answer["next"] = fmt.Sprintf("kubectl %s was stopped at the time limit of an MCP call. What it sent the cluster before that goes on there; check the result with a read (k8s_kubectl \"get …\") before you repeat the change.", verb)
	return answer
}

// handleK8sApply applies a manifest (YAML/JSON) to the cluster. Mutation → confirmed.
func (s *Registry) planK8sApply(_ context.Context, args map[string]any) (*Plan, error) {
	clusterID := stringArg(args, "cluster_id")
	manifest := stringArg(args, "manifest")
	if clusterID == "" || manifest == "" {
		return answered(usageT("cluster_id and manifest are required", "нужны cluster_id и manifest"), true), nil
	}
	// namespace goes to kubectl as an argument: a value starting with '-'
	// would be read as another flag (as for k8s_helm).
	if strings.HasPrefix(stringArg(args, "namespace"), "-") {
		return answered(usageT("'namespace' must not start with '-'", "'namespace' не может начинаться с '-'"), true), nil
	}
	preview := map[string]any{
		"message": i18n.T("k8s_apply changes the cluster.",
			"k8s_apply меняет кластер."),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
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
	}}, nil
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
func (s *Registry) planK8sHelm(_ context.Context, args map[string]any) (*Plan, error) {
	clusterID := stringArg(args, "cluster_id")
	action := strings.ToLower(stringArg(args, "action"))
	if clusterID == "" || action == "" {
		return answered(usageT("cluster_id and action are required (install|upgrade|uninstall|list|status)",
			"нужны cluster_id и action (install|upgrade|uninstall|list|status)"), true), nil
	}
	// release and chart go to helm as positional arguments: a value starting
	// with '-' would be parsed as a flag (--post-renderer runs a program on
	// the server's host).
	for _, k := range []string{"release", "chart", "namespace", "version"} {
		if strings.HasPrefix(stringArg(args, k), "-") {
			return answered(usageT("'%s' must not start with '-'", "'%s' не может начинаться с '-'", k), true), nil
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
			return answered(usageT("install/upgrade need release and chart", "для install и upgrade нужны release и chart"), true), nil
		}
	case "uninstall", "delete":
		if release == "" {
			return answered(usageT("uninstall needs release", "для uninstall нужен release"), true), nil
		}
	case "status":
		if release == "" {
			return answered(usageT("status needs release", "для status нужен release"), true), nil
		}
	case "list":
	default:
		return answered(usageT("unknown action %q (install|upgrade|uninstall|list|status)", "неизвестное действие %q (install|upgrade|uninstall|list|status)", action), true), nil
	}
	// helm itself refused a relative repo_url ("non-absolute URLs"), and
	// that read as an API error (release audit, A-003).
	if raw := stringArg(args, "repo_url"); raw != "" {
		if u, err := url.Parse(raw); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "oci") {
			return answered(usageT("'repo_url' must be an absolute https://, http:// or oci:// URL, got %q",
				"'repo_url' должен быть абсолютным URL https://, http:// или oci://, передано %q", raw), true), nil
		}
	}
	preview := map[string]any{
		"message": i18n.Tf("helm %s changes the cluster (billable workloads).",
			"helm %s меняет кластер (платные нагрузки).", action),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
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
			for _, st := range []struct {
				name string
				own  time.Duration
				args []string
			}{
				{"repo add", 60 * time.Second, []string{"repo", "add", repoName, repoURL, "--force-update"}},
				{"repo update", 90 * time.Second, []string{"repo", "update"}},
			} {
				timeout, capped := callBound(ctx, st.own)
				out, e := kubetools.Helm(ctx, kc, timeout, st.args...)
				if capped && errors.Is(e, context.DeadlineExceeded) && ctx.Err() == nil {
					return toJSON(helmAtLimit(out, st.name, args)), false
				}
				if e != nil {
					return errAnswer(e, map[string]any{"stage": st.name, "output": out}), true
				}
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
					return errResult(fmt.Errorf(i18n.T("values file: %w", "файл values: %w"), e)), true
				}
				defer os.Remove(vf.Name())
				_, e = vf.WriteString(values)
				if ce := vf.Close(); e == nil {
					e = ce
				}
				if e != nil {
					return errResult(fmt.Errorf(i18n.T("values file: %w", "файл values: %w"), e)), true
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
			// The CLI waits for the pods as before (H14). An MCP call does not
			// (D69): helm answers once the cluster took the manifests, the
			// answer says deploying, and the next step reads the release; its
			// own --timeout (hooks) ends within the call's budget, so helm ends
			// a hook itself rather than being killed mid-operation.
			if t, capped := callBound(ctx, helmWaitTimeout); capped {
				hargs = append(hargs, "--timeout", (t - min(t/10, 3*time.Second)).Round(time.Second).String())
			} else {
				hargs = append(hargs, "--wait", "--timeout", "8m")
			}
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

		own := 120 * time.Second
		if action == "install" || action == "upgrade" {
			own = helmWaitTimeout // --wait can take a while pulling images
		}
		timeout, capped := callBound(ctx, own)
		out, err := kubetools.Helm(ctx, kc, timeout, hargs...)
		if capped && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return toJSON(helmAtLimit(out, action, args)), false
		}
		if err != nil {
			return errAnswer(err, map[string]any{"output": out}), true
		}
		if capped && (action == "install" || action == "upgrade") {
			answer := map[string]any{"status": "deploying", "action": action, "output": out,
				"next": fmt.Sprintf("helm %s: the cluster took the release's manifests (helm says deployed); its pods may still be starting — deployed does not mean ready. Read the release with k8s_helm action=status, and the pods with k8s_kubectl \"get pods -n <namespace>\".", action)}
			nextStep(answer, "k8s_helm", helmStatusArgs(args))
			return toJSON(answer), false
		}
		return toJSON(map[string]any{"status": "ok", "action": action, "output": out}), false
	}}, nil
}

// helmWaitTimeout bounds helm install and upgrade in the CLI: --wait
// --timeout 8m and a minute to answer.
const helmWaitTimeout = 9 * time.Minute

// helmStatusArgs are the arguments of k8s_helm that read the release of
// args.
func helmStatusArgs(args map[string]any) map[string]any {
	next := map[string]any{"cluster_id": stringArg(args, "cluster_id"), "action": "status", "release": stringArg(args, "release")}
	if ns := stringArg(args, "namespace"); ns != "" {
		next["namespace"] = ns
	}
	return next
}

// helmAtLimit is the answer of helm stopped at the limit of an MCP call
// (D69): not an error — what it printed so far; a release the stop may
// have left mid-operation is read next.
func helmAtLimit(out, stage string, args map[string]any) map[string]any {
	answer := map[string]any{"status": "stopped_at_limit", "stage": stage, "output": out}
	switch action := strings.ToLower(stringArg(args, "action")); {
	case strings.HasPrefix(stage, "repo "):
		// Before helm touched the cluster: the same call again, with the
		// consent it was given — a declared continuation (owner's
		// decision 12.11; «Риск», conclusion 126).
		answer["next"] = fmt.Sprintf("helm %s was stopped at the time limit of an MCP call, before the release was touched. Call k8s_helm again with the same arguments.", stage)
		nextStep(answer, "k8s_helm", maps.Clone(args))
	case action == "list" || action == "status":
		answer["next"] = fmt.Sprintf("helm %s was stopped at the time limit of an MCP call; it changes nothing. Call k8s_helm again.", action)
		nextSameCall(answer, "k8s_helm", args)
	default:
		answer["next"] = fmt.Sprintf("helm (%s) was stopped at the time limit of an MCP call; the release may be left mid-operation. Read it with k8s_helm action=status: a release in pending-install or pending-upgrade is ended with helm rollback or uninstall.", stage)
		nextStep(answer, "k8s_helm", helmStatusArgs(args))
	}
	return answer
}
