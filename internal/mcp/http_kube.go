// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/kubetools"
)

// kubectl and helm run on the MCP server's host. Over HTTP their arguments
// come from a remote client, so the policy below keeps them on the cluster:
// no host files, no listeners on the host, no plugins from the host's PATH,
// no other API server for the cluster token, no internal URLs.

// kubectlHTTPVerbs are the built-in kubectl commands allowed over HTTP.
// Left out: plugin lookups (any other word), cp (host files), port-forward
// and proxy (listeners on the host), edit (runs an editor on the host),
// kustomize (host directory), attach (needs a terminal), completion, plugin.
var kubectlHTTPVerbs = map[string]bool{
	"get": true, "describe": true, "logs": true, "log": true, "top": true,
	"version": true, "api-resources": true, "api-versions": true, "explain": true,
	"events": true, "cluster-info": true, "config": true, "auth": true,
	"rollout": true, "diff": true, "wait": true,
	"create": true, "expose": true, "run": true, "set": true, "delete": true,
	"scale": true, "autoscale": true, "certificate": true, "cordon": true,
	"uncordon": true, "drain": true, "taint": true, "exec": true, "debug": true,
	"apply": true, "patch": true, "replace": true, "label": true, "annotate": true,
}

// kubectlHTTPDeniedFlags read or write host files, or send the cluster token
// to another server. Names are as kubectl sees them after turning "_" into
// "-" (kubectl accepts --client_key for --client-key).
var kubectlHTTPDeniedFlags = map[string]bool{
	"kubeconfig": true, "server": true, "s": true, "token": true, "username": true,
	"password": true, "client-certificate": true, "client-key": true,
	"certificate-authority": true, "insecure-skip-tls-verify": true,
	"tls-server-name": true, "cache-dir": true, "filename": true, "f": true,
	"kustomize": true, "k": true, "profile": true, "profile-output": true,
	"log-file": true, "log-dir": true, "output-directory": true,
	// create secret tls --cert/--key read a PEM pair from the host;
	// debug --custom reads a container spec from a host file.
	"cert": true, "key": true, "ca-cert": true, "custom": true,
	// --kuberc reads a preferences file of the host: its errors tell
	// whether a path exists, and its aliases and flag defaults would act
	// on the call after this check (release audit, R-05).
	"kuberc": true,
}

// kubectlHTTPVerbosityFlags raise client-go's log level: from -v=6 kubectl
// logs API requests, from -v=8 whole response bodies (Secret data included)
// to stderr, which reaches the caller with the output.
var kubectlHTTPVerbosityFlags = map[string]bool{"v": true, "vmodule": true}

// kubectlHTTPCheck validates k8s_kubectl arguments for the HTTP transport.
// allowSecrets also permits commands that print credentials.
func kubectlHTTPCheck(fields []string, allowSecrets bool) error {
	// The command comes first. This check does not know which flags take a
	// value, kubectl does: in "-n get cp …" it reads "get" as the namespace
	// and runs cp (security audit 19.10, SEC-1).
	if len(fields) > 0 && strings.HasPrefix(fields[0], "-") {
		return fmt.Errorf("k8s_kubectl over HTTP: put the kubectl command first, flags after it (got %q first)", fields[0])
	}
	verb := ""
	var resources, outputs []string
	template, list := false, false
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "--" {
			if verb == "exec" || verb == "debug" || verb == "run" {
				break // the rest is the command run in the pod
			}
			// After "--" every word is positional: "get -o yaml -- secret x"
			// names the resources there.
			for _, p := range fields[i+1:] {
				if verb == "" {
					verb = p
				} else {
					resources = append(resources, p)
				}
			}
			break
		}
		if !strings.HasPrefix(f, "-") || f == "-" {
			if verb == "" {
				verb = f
			} else {
				resources = append(resources, f)
			}
			continue
		}
		for _, fl := range kubetools.ParseFlags(f) {
			name, val := fl.Name, fl.Val
			if kubectlHTTPDeniedFlags[name] || strings.Contains(name, "file") || strings.HasSuffix(name, "-dir") {
				return fmt.Errorf("k8s_kubectl over HTTP: flag %s is not allowed: it reaches the MCP server's own files or credentials (use k8s_apply for manifests)", f)
			}
			if kubectlHTTPVerbosityFlags[name] {
				return fmt.Errorf("k8s_kubectl over HTTP: flag %s is not allowed: at higher verbosity kubectl logs API responses, Secret data included, into the output", f)
			}
			switch name {
			case "o", "output":
				if !fl.HasVal && i+1 < len(fields) {
					i++
					val = fields[i]
				}
				if strings.Contains(val, "file") {
					return fmt.Errorf("k8s_kubectl over HTTP: output %q reads a file on the MCP server", val)
				}
				outputs = append(outputs, val)
			case "template", "go-template":
				template = true // without -o it means -o go-template
			case "list":
				list = true
			case "raw":
				if !allowSecrets {
					return fmt.Errorf("k8s_kubectl over HTTP: --raw is disabled (it can read Secret objects); start the server with --allow-secret-export to enable it")
				}
			case "resolve":
				if !allowSecrets {
					return fmt.Errorf("k8s_kubectl over HTTP: --resolve is disabled (set env --list --resolve prints values from Secrets); start the server with --allow-secret-export to enable it")
				}
			}
		}
	}
	if verb == "" {
		return nil // the handler itself asks for args
	}
	if !kubectlHTTPVerbs[verb] {
		return fmt.Errorf("k8s_kubectl over HTTP: %q is not a built-in kubectl command allowed in this mode (plugins, cp, port-forward, proxy, edit and kustomize run on the MCP server's host); put the command first, flags after it", verb)
	}
	if allowSecrets {
		return nil
	}
	// Not resources[0]: a flag value before the subcommand ("create -n
	// default token sa") lands in resources too (SEC-1).
	if verb == "create" && slices.Contains(resources, "token") {
		return fmt.Errorf("k8s_kubectl create token is disabled over HTTP: it mints a credential; start the server with --allow-secret-export to enable it")
	}
	if !namesSecrets(resources) {
		return nil
	}
	// Any verb that names a Secret may print it: get, but also
	// "label/patch/apply … secret x -o yaml --dry-run=server". Only the
	// table (no -o, -o wide) and -o name leave the data out.
	const hint = "it prints the secret data; start the server with --allow-secret-export to enable it"
	for _, o := range outputs {
		if o != "" && o != "name" && o != "wide" {
			return fmt.Errorf("k8s_kubectl %s secret with -o %s is disabled over HTTP: %s", verb, o, hint)
		}
	}
	switch {
	case template:
		return fmt.Errorf("k8s_kubectl %s secret with --template is disabled over HTTP: %s", verb, hint)
	case list:
		// annotate --list shows the last-applied-configuration annotation,
		// which holds the data of a Secret created with apply.
		return fmt.Errorf("k8s_kubectl %s secret with --list is disabled over HTTP: %s", verb, hint)
	case verb == "apply" && slices.Contains(resources, "view-last-applied"):
		// Prints the same annotation, as YAML by default.
		return fmt.Errorf("k8s_kubectl apply view-last-applied secret is disabled over HTTP: %s", hint)
	}
	return nil
}

// namesSecrets reports whether kubectl resource arguments include Secret
// objects ("secret", "secrets/x", "secret.v1", "cm,secret").
func namesSecrets(resources []string) bool {
	for _, r := range resources {
		for _, part := range strings.Split(r, ",") {
			kind, _, _ := strings.Cut(part, "/")
			kind, _, _ = strings.Cut(strings.ToLower(kind), ".")
			if kind == "secret" || kind == "secrets" {
				return true
			}
		}
	}
	return false
}

// lookupIP resolves a host name; tests replace it.
var lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// helmHTTPCheck validates k8s_helm arguments for the HTTP transport: the
// chart is remote (not a path on the MCP server), and every URL helm will
// fetch is https/oci on a public address — not the container's loopback,
// the cloud metadata service or a private network.
func helmHTTPCheck(ctx context.Context, args map[string]any) error {
	chart, _ := args["chart"].(string)
	repoURL, _ := args["repo_url"].(string)
	if repoURL != "" {
		if err := publicURL(ctx, repoURL); err != nil {
			return fmt.Errorf("k8s_helm over HTTP: repo_url: %w", err)
		}
	}
	if chart == "" {
		return nil
	}
	if strings.Contains(chart, "://") {
		if err := publicURL(ctx, chart); err != nil {
			return fmt.Errorf("k8s_helm over HTTP: chart: %w", err)
		}
		return nil
	}
	// "repo/name" or "name": helm tries a local path of that name first.
	local := strings.HasPrefix(chart, "/") || strings.HasPrefix(chart, ".") ||
		strings.HasPrefix(chart, "~") || strings.Contains(chart, `\`) || strings.Contains(chart, ":")
	if _, err := os.Stat(chart); err == nil {
		local = true
	}
	if local {
		return fmt.Errorf("k8s_helm over HTTP: chart %q is a path on the MCP server; use repo/name with repo_url, or an oci:// / https:// chart", chart)
	}
	return nil
}

// publicURL accepts an https or oci URL whose host resolves only to public
// unicast addresses.
func publicURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q", raw)
	}
	if u.Scheme != "https" && u.Scheme != "oci" {
		return fmt.Errorf("%q: only https:// and oci:// URLs are allowed in this mode", raw)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%q has no host", raw)
	}
	ips := []net.IP{net.ParseIP(host)}
	if ips[0] == nil {
		if ips, err = lookupIP(ctx, host); err != nil {
			return fmt.Errorf("resolve %s: %w", host, err)
		}
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return fmt.Errorf("%s resolves to %s, a loopback, private or link-local address", host, ip)
		}
	}
	return nil
}

// publicIP reports whether ip is a global unicast address outside private,
// shared (100.64/10, CGNAT) and link-local ranges.
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || !ip.IsGlobalUnicast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 {
		return false
	}
	return true
}
