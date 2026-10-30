// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import clerrors "github.com/shatim-ops/evo-cli/internal/errors"

// refusals is the rule of a wait or a retry after the machine slept
// (process backlog 1.6, priority 1): one refused token is the token's —
// the clock of a paused machine is behind, the provider has forgotten the
// token (G140-3) and the next request takes a new one — so the wait goes
// on; refused twice in a row is the key's, and that ends it.
type refusals struct{ last bool }

// keyRefused records the result of a request and reports whether the key
// is refused: err is a refused token and so was the one before.
func (r *refusals) keyRefused(err error) bool {
	refused := refusedToken(err)
	again := refused && r.last
	r.last = refused
	return again
}

// refusedToken: the service refused the token (401) or the key.
func refusedToken(err error) bool {
	reason := clerrors.Reason(err)
	return err != nil && (reason == clerrors.CodeAuthExpired || reason == clerrors.CodeAuthFailed)
}
