// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package product holds the product's display name and authorship. The
// binary, the MCP server name, the environment variables and the config
// paths stay "evo".
package product

import "sync/atomic"

// Name is the product (D34): evo version, the MCP serverInfo title, the
// User-Agent and the release name (.goreleaser.yaml). Always whole: the
// abbreviation alone names another CLI for MCP servers (PyPI), so it never
// stands without "evo" (TestNoBareCMCP).
const Name = "evo CMCP"

// TitleEN and TitleRU name it with what it is: the first line of evo
// --help, in the CLI's language (i18n.T(TitleEN, TitleRU)). MCP sends
// English only (D6), so it gets Name.
const (
	TitleEN = Name + " — CLI and MCP server for Cloud.ru Evolution"
	TitleRU = Name + " — CLI и MCP-сервер для Cloud.ru Evolution"
)

// Feedback goes to the author and the testers' chat in Telegram; shown in
// evo --help and README.
const (
	FeedbackContact = "https://t.me/shatim"
	FeedbackChat    = "https://t.me/+SYvXziA4G9c1Y2Ni"
)

// Author is who made evo. It is in evo version, the hidden "evo crew",
// NOTICE (Apache 2.0, section 4(d): a redistribution keeps NOTICE) and the
// SPDX header of every Go file; TestAuthorship keeps them in step.
const (
	Author   = "Тимур Шакирбеков, Cloud.ru"
	AuthorEN = "Timur Shakirbekov, Cloud.ru"
	Repo     = "https://github.com/shatim-ops/evo-cli"
)

var version atomic.Value // string

// SetVersion records the build version for UserAgent; the CLI sets it at
// start.
func SetVersion(v string) { version.Store(v) }

// UserAgent is the User-Agent of evo's requests to Cloud.ru: the product
// and where it comes from, so the platform side can tell evo traffic (and
// a copy of its clients) from the rest.
func UserAgent() string {
	v, _ := version.Load().(string)
	if v == "" {
		v = "dev"
	}
	return "evo/" + v + " (" + Name + "; +" + Repo + ")"
}
