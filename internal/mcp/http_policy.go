// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/kubetools"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// binaryAvailable reports whether kubectl/helm can be run; tests replace it
// (kubetools also looks in /usr/local/bin, where CI runners have both).
var binaryAvailable = kubetools.Available

// Restriction is one entry of the closed list of ways evo mcp --http
// behaves differently from stdio: docs/mcp.md, «Ограничения HTTP-режима»,
// has a row per ID. The tool set and the schemas are the same on both
// transports; only the answers of some calls differ, and only as listed.
// The server settings an HTTP client meets are listed too (StdioToo:
// --allow-project, --read-only).
type Restriction struct {
	ID string // H1…H11, the row in docs/mcp.md
	// Tools are the tools whose handler restrictForHTTP wraps for the
	// entry; AllTools wraps every tool. With neither, the difference is
	// not in a tool handler and Where says where it is.
	Tools    []string
	AllTools bool
	// UnlessSecretExport: only without --allow-secret-export.
	UnlessSecretExport bool
	// StdioToo: a server setting that holds over stdio as well; listed
	// because an HTTP client meets it. NewHTTPHandler does not apply it.
	StdioToo bool
	// ReadOnlyToo: a --read-only server applies it on either transport,
	// as if without --allow-secret-export, and words its refusals for
	// --read-only (readOnlyWording).
	ReadOnlyToo bool
	// Where is the file (from the repository root) and Symbol a name or
	// text in it that implements the entry.
	Where, Symbol string
	Summary       string // what the client sees
}

// HTTPRestrictions is the closed list. restrictForHTTP wraps a handler only
// through an entry here (restrict panics otherwise), and
// TestHTTPPolicy_ClosedList checks the list against the handlers and
// against docs/mcp.md.
var HTTPRestrictions = []Restriction{
	{ID: "H1", Tools: []string{"k8s_apply", "k8s_helm", "k8s_kubectl"},
		Where: "internal/mcp/http_policy.go", Symbol: "binaryAvailable",
		Summary: "CONFIG_INVALID when kubectl/helm is not installed on the server (the Docker image ships without them)"},
	{ID: "H2", Tools: []string{"deploy"},
		Where: "internal/mcp/http_paths.go", Symbol: "insideWorkDir",
		Summary: "compose_file only inside the server's working directory"},
	{ID: "H3", Tools: []string{"k8s_kubectl"}, ReadOnlyToo: true,
		Where: "internal/mcp/http_kube.go", Symbol: "kubectlCheck",
		Summary: "the kubectl command first; no host files, plugins, editors, verbose logs, other servers; a Secret only as a table or -o name without --allow-secret-export"},
	{ID: "H4", Tools: []string{"k8s_helm"},
		Where: "internal/mcp/http_kube.go", Symbol: "helmHTTPCheck",
		Summary: "remote charts on public https/oci hosts only"},
	{ID: "H5", Tools: []string{"cert_private_key", "secret_value"}, UnlessSecretExport: true,
		Where: "internal/mcp/http_policy.go", Symbol: "is disabled over HTTP: it returns",
		Summary: "disabled: they return a secret in plaintext"},
	{ID: "H6", Tools: []string{"k8s_kubeconfig", "k8s_kubectl"}, UnlessSecretExport: true, ReadOnlyToo: true,
		Where: "internal/mcp/http_policy.go", Symbol: "the token grants access to the cluster",
		Summary: "k8s_kubeconfig with embed_token=true and k8s_kubectl config are disabled: they return the cluster token"},
	{ID: "H7", AllTools: true, UnlessSecretExport: true, ReadOnlyToo: true,
		Where: "internal/mcp/http_redact.go", Symbol: "HTTPSecretFields",
		Summary: "fields with a root of HTTPSecretFields, except HTTPNonSecretFields, are masked; the result lists the masked fields in redacted"},
	{ID: "H8",
		Where: "internal/mcp/http.go", Symbol: "httpMaxToolCalls",
		Summary: "at most 32 tool calls at once; the next one gets SERVER_BUSY"},
	{ID: "H9", StdioToo: true,
		Where: "internal/cli/mcp.go", Symbol: "checkAllowedProject",
		Summary: "--allow-project: a call in another project gets CONFIG_INVALID; a read wider than a project with no project_id (billing_consumption, billing_finops, org_projects) reads the server's project only (tools.Registry.SetProjectBound)"},
	{ID: "H10", Tools: []string{"doctor"},
		Where: "internal/mcp/http_policy.go", Symbol: "doctor is disabled over HTTP",
		Summary: "disabled: its checks name the server's files and network (paths in the configuration's warnings, addresses in network errors)"},
	{ID: "H11", AllTools: true, StdioToo: true,
		Where: "internal/mcp/readonly.go", Symbol: "tools.NeedsConsent",
		Summary: "--read-only: a call that needs consent (confirmed=true) gets USAGE, confirmed or not, and tools/list leaves out the tools every call of which needs it"},
}

// wording words the refusals (and the mask of H7) of a restriction a
// server applies for one of two reasons: its client is remote (over HTTP
// without --allow-secret-export) or it runs with --read-only, on either
// transport.
type wording struct {
	where   string // where the call is refused
	restart string // what lifts the restriction, a clause in lower case
	mask    string // what replaces a credential in a result (H7)
}

var (
	httpWording = wording{where: "over HTTP", restart: "start the server with --allow-secret-export",
		mask: redacted}
	readOnlyWording = wording{where: "on this --read-only server", restart: "ask the server's operator to restart it without --read-only",
		mask: "[redacted on this --read-only server]"}
)

// restartSentence is restart as a sentence of its own.
func (w wording) restartSentence() string {
	return strings.ToUpper(w.restart[:1]) + w.restart[1:]
}

// restriction returns the entry of HTTPRestrictions with the given ID.
func restriction(id string) Restriction {
	for _, r := range HTTPRestrictions {
		if r.ID == id {
			return r
		}
	}
	panic("mcp: no HTTP restriction " + id)
}

// restrict wraps a tool's handler for an entry of HTTPRestrictions. A wrap
// the entry does not list is a programming error: the list in docs/mcp.md
// would no longer be the whole list.
func (s *Server) restrict(id, tool string, wrap func(next tools.Handler) tools.Handler) {
	r := restriction(id)
	if !r.AllTools && !slices.Contains(r.Tools, tool) {
		panic(fmt.Sprintf("mcp: HTTP restriction %s does not list %s (HTTPRestrictions)", id, tool))
	}
	s.httpApplied = append(s.httpApplied, id+" "+tool)
	s.wrapHandler(tool, wrap)
}

// restrictForHTTP adjusts tool handlers for the HTTP transport, as
// HTTPRestrictions lists (H1–H7, H10; H8 is in the HTTP handler, H9 in the
// CLI). A --read-only server has its H3, H6 and H7 already (SetReadOnly)
// and exports no secret, so H5 would refuse nothing H11 lets through; the
// wraps here go under its H11 gate (wrapHandler).
// The tool set and schemas stay the same; some calls answer with an error
// instead, and without allowSecretExport credential fields are masked.
//
// Wrapping the handlers (not the tools/call dispatch) also covers calls the
// stack orchestrator makes in-process.
func (s *Server) restrictForHTTP(allowSecretExport bool) {
	// H1: kubectl/helm must be installed on the server.
	needsBinary := map[string]string{
		"k8s_kubectl": "kubectl",
		"k8s_apply":   "kubectl",
		"k8s_helm":    "helm",
	}
	for _, tool := range restriction("H1").Tools {
		bin := needsBinary[tool]
		s.restrict("H1", tool, func(next tools.Handler) tools.Handler {
			return func(ctx context.Context, args map[string]any) (string, bool) {
				if !binaryAvailable(bin) {
					return tools.ErrResult(clierrors.New(clierrors.CodeConfigInvalid, fmt.Sprintf(
						"%s is not available in this mode: %s is not installed on the MCP server (the Docker image ships without kubectl/helm)", tool, bin),
						fmt.Sprintf("Use k8s_kubeconfig with embed_token=false and run %s on your machine, or run 'evo mcp' over stdio on a host with %s", bin, bin))), true
				}
				return next(ctx, args)
			}
		})
	}

	// H2–H4: arguments that name files or URLs reach the server's own
	// filesystem and network: confine them (http_paths.go, http_kube.go).
	s.restrict("H2", "deploy", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			if p, _ := args["compose_file"].(string); p != "" {
				if err := insideWorkDir(p); err != nil {
					return refused(fmt.Errorf("compose_file over HTTP: %w", err)), true
				}
			}
			return next(ctx, args)
		}
	})
	if !s.readOnly {
		s.restrictKubectl(allowSecretExport, httpWording)
	}
	s.restrict("H4", "k8s_helm", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			if err := helmHTTPCheck(ctx, args); err != nil {
				return refused(err), true
			}
			return next(ctx, args)
		}
	})

	// H10: doctor checks the server itself, and its checks name the
	// server's files (the configuration's warnings: credentials.yaml,
	// .evo.yaml) and network (a resolver's or a proxy's address in a
	// network error). whoami names neither.
	s.restrict("H10", "doctor", func(tools.Handler) tools.Handler {
		return func(context.Context, map[string]any) (string, bool) {
			return refused(fmt.Errorf(
				"doctor is disabled over HTTP: its checks name the server's files and network. " +
					"Call whoami for the key and the project; the server's operator runs 'evo doctor' on the server")), true
		}
	})

	if allowSecretExport || s.readOnly {
		return
	}
	s.restrictSecrets(httpWording)
	// H5: tools that return a secret are disabled.
	s.restrict("H5", "secret_value", func(tools.Handler) tools.Handler {
		return func(context.Context, map[string]any) (string, bool) {
			return refused(fmt.Errorf(
				"secret_value is disabled over HTTP: it returns a decrypted secret. " +
					"Start the server with --allow-secret-export to enable it")), true
		}
	})
	s.restrict("H5", "cert_private_key", func(tools.Handler) tools.Handler {
		return func(context.Context, map[string]any) (string, bool) {
			return refused(fmt.Errorf(
				"cert_private_key is disabled over HTTP: it returns a private key in plaintext. " +
					"Start the server with --allow-secret-export to enable it")), true
		}
	})
}

// restrictKubectl confines the arguments of k8s_kubectl (H3): over HTTP,
// and on a --read-only server, where secrets are not exported
// (allowSecretExport false). w words the refusals.
func (s *Server) restrictKubectl(allowSecretExport bool, w wording) {
	s.restrict("H3", "k8s_kubectl", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			kargs, _ := args["args"].(string)
			if err := kubectlCheck(strings.Fields(kargs), allowSecretExport, w); err != nil {
				return refused(err), true
			}
			return next(ctx, args)
		}
	})
}

// restrictSecrets keeps credentials on the server (H7, H6): over HTTP
// without --allow-secret-export, and on a --read-only server. w words the
// refusals and the mask.
func (s *Server) restrictSecrets(w wording) {
	// H7: any tool may echo a cloud object that carries a credential (a
	// cluster's password, a generated VM password in deploy_apply): the
	// credential fields (http_redact.go) are masked in every result. Wrapped before
	// H5/H6, so their refusals sit on top.
	for _, t := range s.reg.Tools() {
		s.restrict("H7", t.Name, func(next tools.Handler) tools.Handler {
			return func(ctx context.Context, args map[string]any) (string, bool) {
				text, isErr := next(ctx, args)
				return redactSecrets(t.Name, text, w), isErr
			}
		})
	}
	// H6: the cluster token stays on the server.
	s.restrict("H6", "k8s_kubeconfig", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			if tools.BoolArg(args, "embed_token", true) {
				return refused(fmt.Errorf(
					"k8s_kubeconfig with an embedded token is disabled %s: the token grants access to the cluster. "+
						"Call it with embed_token=false (kubeconfig with the cloudlogin exec plugin, no credentials inside), "+
						"or %s", w.where, w.restart)), true
			}
			return next(ctx, args)
		}
	})
	// k8s_kubectl runs on a kubeconfig with the same embedded token, and
	// "kubectl config view --raw" prints it.
	s.restrict("H6", "k8s_kubectl", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			kargs, _ := args["args"].(string)
			if slices.Contains(strings.Fields(kargs), "config") {
				return refused(fmt.Errorf(
					"k8s_kubectl config is disabled %s: the kubeconfig holds the cluster token. "+
						"%s to enable it", w.where, w.restartSentence())), true
			}
			return next(ctx, args)
		}
	})
}

// refused is the answer to a call the HTTP policy or --read-only refuses:
// USAGE, as a call refused for its arguments, since nothing reached the
// cloud and the same call over stdio without --read-only (or with another
// argument) would run.
func refused(err error) string {
	return tools.ErrResult(clierrors.Wrap(clierrors.CodeUsage, err.Error(), "", err))
}

// wrapHandler wraps a tool's handler in the registry, so the policy also
// holds for in-process calls (stack_apply drives other tools), and mirrors
// the result into the tools/call dispatch table. Under the H11 gate of a
// --read-only server (Server.gated) it wraps the handler the gate calls,
// and the gate stays the outermost layer.
func (s *Server) wrapHandler(name string, wrap func(next tools.Handler) tools.Handler) {
	if inner, ok := s.gated[name]; ok {
		*inner = wrap(*inner)
		return
	}
	if !s.reg.Wrap(name, wrap) {
		return
	}
	if t, ok := s.reg.Lookup(name); ok {
		s.handlers[name] = toolHandler(t.Handler)
	}
}
