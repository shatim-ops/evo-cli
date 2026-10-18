// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import "net/url"

// seg escapes a resource id for one URL path segment. Ids come from MCP
// arguments: "x/../y" or "x?y" must not address another resource than the
// one a confirmation preview named, and "." / ".." are not ids either.
func seg(id string) string {
	switch id {
	case ".":
		return "%2E"
	case "..":
		return "%2E%2E"
	}
	return url.PathEscape(id)
}
