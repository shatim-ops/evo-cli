// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"net"
	"net/http/httptrace"
	"sync/atomic"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// A failed dial, proxy connection, or TLS handshake precedes the HTTP
// request. A later 404 cannot attribute the disappearance to this call.
func deleteConnection(ctx context.Context, tool string) (context.Context, func(error) bool) {
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
		if !refused && !clierrors.TLSUntrusted(err) && !(connecting.Load() && !connected.Load()) {
			return false
		}
		if c, ok := ctx.Value(callRecordsKey{}).(*callRecords); ok && c.hasOpen && c.open.Tool == tool {
			c.deleteUnsent = true
		}
		return true
	}
}

func unsentDeleteAnswer(err error) (string, bool) {
	_, _, hint := errReason(err, nil)
	return errAnswer(clierrors.Wrap(clierrors.CodeAPIUnreachable,
		i18n.T("The connection failed before the delete request was sent.", "Не удалось установить соединение для отправки запроса на удаление."), hint, err), nil), true
}

// deleteObservation describes the service read independently of who acted.
func deleteObservation() string {
	return i18n.T("The resource is now absent. The service did not acknowledge this delete request.", "При проверке ресурс отсутствовал. Ответ сервиса на запрос удаления не получен.")
}
