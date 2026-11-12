// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strconv"
	"sync/atomic"
)

// iamWorlds counts the fake IAMs of the package's tests.
var iamWorlds atomic.Int64

// iamTokens are the tokens one fake IAM hands out: tok-<world>-1, -2, …,
// never the token of another world. The providers of the process are one
// list (auth.renewRefused finds the one that issued a refused token by its
// value): with tok-1 in every world, a live provider of an earlier test
// answered the renewal of this test's tok-1 with its own token, and the
// create went through without the second exchange (ci of main, ubuntu,
// -shuffle 1794451395583592205; macOS and Windows before, process backlog
// «красные ci не от diff», classes (б) and (в)).
type iamTokens struct{ prefix string }

// token is the n-th token of the world (from 1).
func (w *iamTokens) token(n int) string {
	if w.prefix == "" {
		w.prefix = "tok-" + strconv.FormatInt(iamWorlds.Add(1), 10) + "-"
	}
	return w.prefix + strconv.Itoa(n)
}
