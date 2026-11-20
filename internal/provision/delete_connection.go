// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"errors"
	"net"
	"net/http/httptrace"
	"sync/atomic"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// TraceDeleteConnection reports whether a transport failure happened
// before the connection for this request was established.
func TraceDeleteConnection(ctx context.Context) (context.Context, func(error) bool) {
	var connecting, connected atomic.Bool
	trace := &httptrace.ClientTrace{
		GetConn: func(string) { connecting.Store(true) },
		GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
	}
	return httptrace.WithClientTrace(ctx, trace), func(err error) bool {
		if clierrors.Reason(err) != clierrors.CodeAPIUnreachable {
			return false
		}
		var op *net.OpError
		refused := errors.As(err, &op) && (op.Op == "dial" || op.Op == "proxyconnect")
		return refused || clierrors.TLSUntrusted(err) || (connecting.Load() && !connected.Load())
	}
}
