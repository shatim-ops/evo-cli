// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// SetReadOnly makes the server read-only (evo mcp --read-only, owner's
// decision Q12 in spec.md §8), on either transport: it makes no call that
// needs the user's consent and hands out no secret. Unlike --toolsets this
// is a permission boundary, for an agent nobody watches: such an agent sets
// confirmed=true itself and its client drops the annotations, so the gate
// of a tool stops nothing there.
//
//   - H11: every call that needs consent (tools.NeedsConsent) is refused
//     with USAGE before its handler, confirmed or not — no engine, no
//     preview, no request — and so are the calls the stack tools make
//     in-process; stack_status reports a running stack instead of
//     advancing it, and deploy_plan shows its plan without writing the
//     deploy key or the plan, so with no plan_id
//     (tools.Registry.SetReadOnly). tools/list leaves out the tools every
//     call of which needs consent (tools.AlwaysNeedsConsent); k8s_kubectl
//     and k8s_helm stay for their reads.
//   - H3, H6, H7 hold on stdio too, as over HTTP without
//     --allow-secret-export: no kubectl command that prints a credential,
//     no cluster token, credential fields masked (readOnlyWording).
//
// Call it before the server serves. H11 is the outermost layer of every
// handler but the check of the arguments (tools.Registry.Wrap), on either
// transport: a call with arguments the tool does not take gets that USAGE
// (G13-1), and a call that needs consent is refused as such before any
// other check — over HTTP too, where the restrictions of the transport
// (NewHTTPHandler) come later and go under it (wrapHandler), so a refused
// call reaches no host file (H2) and no resolver (H4).
func (s *Server) SetReadOnly() {
	if s.readOnly {
		return
	}
	s.readOnly = true
	s.reg.SetReadOnly()
	s.restrictKubectl(false, readOnlyWording)
	s.restrictSecrets(readOnlyWording)
	s.gated = map[string]*tools.Handler{}
	for _, t := range s.reg.Tools() {
		s.restrict("H11", t.Name, func(next tools.Handler) tools.Handler {
			inner := &next
			s.gated[t.Name] = inner
			return func(ctx context.Context, args map[string]any) (string, bool) {
				if tools.NeedsConsent(t, args) {
					return readOnlyRefusal(t.Name), true
				}
				return (*inner)(ctx, args)
			}
		})
	}
	s.relist()
}

// readOnlyRefusal is the answer of a call H11 refuses.
func readOnlyRefusal(tool string) string {
	return tools.ErrResult(clierrors.New(clierrors.CodeUsage,
		tool+" is refused: this server runs with --read-only, and the call needs the user's consent (confirmed=true), "+
			"as it changes the cloud or reveals a secret; a read-only server makes no such call, confirmed or not",
		"Calls that only read work as usual. A change needs a server without --read-only: ask its operator."))
}
