// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"net"
	"net/http"
	"net/url"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// ProxyFunc is the proxy http.DefaultTransport picks for a request
// (HTTPS_PROXY, HTTP_PROXY, NO_PROXY). Every client of evo goes through
// that transport; tests replace the function.
var ProxyFunc = http.ProxyFromEnvironment

// NetworkHint is the hint of a request that got no answer from Cloud.ru,
// from its transport error err:
//   - a certificate the system does not trust — a corporate proxy that
//     inspects HTTPS shows its own: how to add its root certificate;
//   - no root certificates at all (a minimal container image);
//   - a request that went through a proxy: which one, and that NO_PROXY
//     exists;
//   - a host name that did not resolve, without a proxy;
//   - the client's timeout: the service did not answer in time;
//   - else fallback (also for a cancelled request).
//
// The hint used to be "check your network connection" for all of them, and
// the cause was hidden: "IAM недоступен" behind an unknown CA or a proxy that
// refused the connection.
func NetworkHint(err error, fallback string) string {
	if err == nil || stderrors.Is(err, context.Canceled) {
		return fallback
	}
	host, proxy := requestHosts(err)
	var roots x509.SystemRootsError
	if stderrors.As(err, &roots) {
		return i18n.Tf("No root certificates were found on this system (%s): install the ca-certificates package or point SSL_CERT_FILE at a CA bundle.",
			"В системе не найдены корневые сертификаты (%s): установите пакет ca-certificates или укажите файл сертификатов в SSL_CERT_FILE.", roots.Error())
	}
	if cause := untrustedCause(err); cause != "" {
		if host == "" {
			host = "Cloud.ru"
		}
		return i18n.Tf("The TLS certificate of %s is not trusted (%s). In a corporate network a proxy that inspects HTTPS shows its own certificate: "+
			"add its root certificate — on Linux SSL_CERT_FILE=/path/to/ca.pem (or SSL_CERT_DIR), on macOS and Windows to the system certificate store. "+
			"evo does not turn certificate checks off.",
			"Сертификат TLS у %s не прошёл проверку (%s). В корпоративной сети так бывает, когда прокси проверяет HTTPS и показывает свой сертификат: "+
				"добавьте корневой сертификат прокси — в Linux SSL_CERT_FILE=/путь/к/ca.pem (или SSL_CERT_DIR), в macOS и Windows — в системное хранилище сертификатов. "+
				"Проверку сертификатов evo не отключает.", host, cause)
	}
	var op *net.OpError
	if proxy != "" || (stderrors.As(err, &op) && op.Op == "proxyconnect") {
		via := i18n.T("a proxy", "прокси")
		if proxy != "" {
			via = i18n.Tf("the proxy %s", "прокси %s", proxy)
		}
		return i18n.Tf("The request went through %s (HTTPS_PROXY / HTTP_PROXY) and failed: %s. Check the proxy address and that it lets the request through; hosts that must bypass it go in NO_PROXY.",
			"Запрос шёл через %s (HTTPS_PROXY / HTTP_PROXY) и не прошёл: %s. Проверьте адрес прокси и доступ через него; адреса, которым прокси не нужен, — в NO_PROXY.",
			via, transportCause(err))
	}
	var dns *net.DNSError
	if stderrors.As(err, &dns) && dns.IsNotFound {
		return i18n.Tf("The name %s did not resolve: check the network and DNS. A network that reaches the internet only through a proxy needs HTTPS_PROXY.",
			"Имя %s не найдено в DNS: проверьте сеть и DNS. Если в интернет можно выйти только через прокси, задайте его в HTTPS_PROXY.", dns.Name)
	}
	// The client's own timeout: the connection was made or tried, and the
	// service did not answer in time. The network may be fine; «check your
	// network connection» sent the person to the wrong place (Grok rc.1).
	var timeout net.Error
	if stderrors.Is(err, context.DeadlineExceeded) || stderrors.As(err, &timeout) && timeout.Timeout() {
		if host == "" {
			host = "Cloud.ru"
		}
		return i18n.Tf("%s did not answer in time: the network may be fine and the service slow. Retry later; if it keeps happening, check the network.",
			"%s не ответил вовремя: сеть может быть в порядке, а сервис — медленным. Повторите позже; если повторяется — проверьте сеть.", host)
	}
	return fallback
}

// TLSUntrusted reports a certificate check that failed: the request never
// left the machine, and repeating it will not help.
func TLSUntrusted(err error) bool {
	var roots x509.SystemRootsError
	return stderrors.As(err, &roots) || untrustedCause(err) != ""
}

// untrustedCause is the text of the certificate error in err ("x509:
// certificate signed by unknown authority"), "" without one.
func untrustedCause(err error) string {
	var verify *tls.CertificateVerificationError
	if stderrors.As(err, &verify) && verify.Err != nil {
		return verify.Err.Error()
	}
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var name x509.HostnameError
	switch {
	case stderrors.As(err, &unknown):
		return unknown.Error()
	case stderrors.As(err, &invalid):
		return invalid.Error()
	case stderrors.As(err, &name):
		return name.Error()
	}
	return ""
}

// requestHosts is the host of the request behind err and the proxy it went
// through (host:port, without credentials), from the *url.Error the HTTP
// client returns.
func requestHosts(err error) (host, proxy string) {
	var ue *url.Error
	if !stderrors.As(err, &ue) {
		return "", ""
	}
	u, perr := url.Parse(ue.URL)
	if perr != nil {
		return "", ""
	}
	if p, perr := ProxyFunc(&http.Request{URL: u}); perr == nil && p != nil {
		proxy = p.Host
	}
	return u.Hostname(), proxy
}

// transportCause is what the transport said, without the method and URL the
// HTTP client puts in front.
func transportCause(err error) string {
	var ue *url.Error
	if stderrors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	return err.Error()
}
