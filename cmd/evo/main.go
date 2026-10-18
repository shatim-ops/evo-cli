// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Command evo is the Cloud.ru infrastructure CLI and MCP server; the code is in internal/cli.
package main

import "github.com/shatim-ops/evo-cli/internal/cli"

// Build identity, set by the release build:
//
//	-ldflags "-X main.version=1.2.3 -X main.commit=<sha> -X main.date=<RFC 3339>"
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() { cli.Main(version, commit, date) }
