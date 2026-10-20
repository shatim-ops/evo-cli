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
type Restriction struct {
	ID string // H1…H9, the row in docs/mcp.md
	// Tools are the tools whose handler restrictForHTTP wraps for the
	// entry; AllTools wraps every tool. With neither, the difference is
	// not in a tool handler and Where says where it is.
	Tools    []string
	AllTools bool
	// UnlessSecretExport: only without --allow-secret-export.
	UnlessSecretExport bool
	// StdioToo: a server setting that holds over stdio as well; listed
	// because an HTTP client meets it.
	StdioToo bool
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
	{ID: "H3", Tools: []string{"k8s_kubectl"},
		Where: "internal/mcp/http_kube.go", Symbol: "kubectlHTTPCheck",
		Summary: "the kubectl command first; no host files, plugins, editors, verbose logs, other servers; a Secret only as a table or -o name without --allow-secret-export"},
	{ID: "H4", Tools: []string{"k8s_helm"},
		Where: "internal/mcp/http_kube.go", Symbol: "helmHTTPCheck",
		Summary: "remote charts on public https/oci hosts only"},
	{ID: "H5", Tools: []string{"cert_private_key", "secret_value"}, UnlessSecretExport: true,
		Where: "internal/mcp/http_policy.go", Symbol: "is disabled over HTTP: it returns",
		Summary: "disabled: they return a secret in plaintext"},
	{ID: "H6", Tools: []string{"k8s_kubeconfig", "k8s_kubectl"}, UnlessSecretExport: true,
		Where: "internal/mcp/http_policy.go", Symbol: "the token grants access to the cluster",
		Summary: "k8s_kubeconfig with embed_token=true and k8s_kubectl config are disabled: they return the cluster token"},
	{ID: "H7", AllTools: true, UnlessSecretExport: true,
		Where: "internal/mcp/http_redact.go", Symbol: "HTTPSecretFields",
		Summary: "fields with a root of HTTPSecretFields, except HTTPNonSecretFields, are masked; the result lists the masked fields in redacted"},
	{ID: "H8",
		Where: "internal/mcp/http.go", Symbol: "httpMaxToolCalls",
		Summary: "at most 32 tool calls at once; the next one gets SERVER_BUSY"},
	{ID: "H9", StdioToo: true,
		Where: "internal/cli/mcp.go", Symbol: "checkAllowedProject",
		Summary: "--allow-project: a call in another project gets CONFIG_INVALID"},
	{ID: "H10", Tools: []string{"doctor"},
		Where: "internal/mcp/http_policy.go", Symbol: "doctor is disabled over HTTP",
		Summary: "disabled: its checks name the server's files and network (paths in the configuration's warnings, addresses in network errors)"},
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
// CLI).
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
	s.restrict("H3", "k8s_kubectl", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			kargs, _ := args["args"].(string)
			if err := kubectlHTTPCheck(strings.Fields(kargs), allowSecretExport); err != nil {
				return refused(err), true
			}
			return next(ctx, args)
		}
	})
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

	if allowSecretExport {
		return
	}
	// H7: any tool may echo a cloud object that carries a credential (a
	// cluster's password, a generated VM password in deploy_apply): the
	// credential fields (http_redact.go) are masked in every result. Wrapped before
	// H5/H6, so their refusals sit on top.
	for _, t := range s.reg.Tools() {
		s.restrict("H7", t.Name, func(next tools.Handler) tools.Handler {
			return func(ctx context.Context, args map[string]any) (string, bool) {
				text, isErr := next(ctx, args)
				return redactSecrets(t.Name, text), isErr
			}
		})
	}
	// H5, H6: tools that return a secret are disabled.
	s.restrict("H5", "secret_value", func(tools.Handler) tools.Handler {
		return func(context.Context, map[string]any) (string, bool) {
			return refused(fmt.Errorf(
				"secret_value is disabled over HTTP: it returns a decrypted secret. " +
					"Start the server with --allow-secret-export to enable it")), true
		}
	})
	s.restrict("H6", "k8s_kubeconfig", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			if tools.BoolArg(args, "embed_token", true) {
				return refused(fmt.Errorf(
					"k8s_kubeconfig with an embedded token is disabled over HTTP: the token grants access to the cluster. " +
						"Call it with embed_token=false (kubeconfig with the cloudlogin exec plugin, no credentials inside), " +
						"or start the server with --allow-secret-export")), true
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
					"k8s_kubectl config is disabled over HTTP: the kubeconfig holds the cluster token. " +
						"Start the server with --allow-secret-export to enable it")), true
			}
			return next(ctx, args)
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

// refused is the answer to a call the HTTP policy refuses: USAGE, as a call
// refused for its arguments, since nothing reached the cloud and the same
// call over stdio (or with another argument) would run.
func refused(err error) string {
	return tools.ErrResult(clierrors.Wrap(clierrors.CodeUsage, err.Error(), "", err))
}

// wrapHandler wraps a tool's handler in the registry, so the policy also
// holds for in-process calls (stack_apply drives other tools), and mirrors
// the result into the tools/call dispatch table.
func (s *Server) wrapHandler(name string, wrap func(next tools.Handler) tools.Handler) {
	if !s.reg.Wrap(name, wrap) {
		return
	}
	if t, ok := s.reg.Lookup(name); ok {
		s.handlers[name] = toolHandler(t.Handler)
	}
}
