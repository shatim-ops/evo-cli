// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import "net/url"

// seg escapes a cluster or operation id for one URL path segment: the id
// comes from MCP arguments, and "x/../y" must not reach another resource
// than the one a confirmation preview named.
func seg(id string) string {
	switch id {
	case ".":
		return "%2E"
	case "..":
		return "%2E%2E"
	}
	return url.PathEscape(id)
}
