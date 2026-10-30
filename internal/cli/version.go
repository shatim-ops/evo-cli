// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/product"
	"github.com/spf13/cobra"
)

// setBuildIdentity sets the build identity Main got from cmd/evo, with the
// fallback to the module and VCS data applied to the variables themselves:
// the MCP server reports the same version string in initialize.serverInfo.
func setBuildIdentity(v, c, d string) {
	info, _ := debug.ReadBuildInfo()
	version, commit, date = resolveVersion(v, c, d, info)
	product.SetVersion(version)
}

// resolveVersion keeps values set through -ldflags and replaces the defaults
// with what the toolchain embedded: the module version for
// "go install …@v1.2.3" (a pseudo-version, "+dirty" for a changed tree, for
// a local build), vcs.revision and vcs.time for a build from a checkout.
// Without either the defaults stay.
func resolveVersion(v, c, d string, info *debug.BuildInfo) (string, string, string) {
	if info == nil {
		return v, c, d
	}
	if v == "dev" {
		if mv := info.Main.Version; mv != "" && mv != "(devel)" {
			v = strings.TrimPrefix(mv, "v")
		}
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if c == "none" && s.Value != "" {
				c = s.Value
			}
		case "vcs.time":
			if d == "unknown" && s.Value != "" {
				d = s.Value
			}
		}
	}
	return v, c, d
}

// versionInfo is the payload of "evo version --json".
type versionInfo struct {
	Product string `json:"product"`
	Author  string `json:"author"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func currentVersion() versionInfo {
	return versionInfo{
		Product: product.Name,
		Author:  i18n.T(product.AuthorEN, product.Author),
		Version: version,
		Commit:  commit,
		Date:    date,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}

// printVersion writes v as text or, with jsonMode, as one JSON object.
func printVersion(w io.Writer, jsonMode bool, v versionInfo) error {
	if jsonMode {
		return output.NewWithWriter(w, output.ModeJSON, false).JSON(v)
	}
	_, err := fmt.Fprintf(w, i18n.T("%s %s\n  author:  %s\n  commit:  %s\n  built:   %s\n  go:      %s\n  os/arch: %s/%s\n",
		"%s %s\n  автор:   %s\n  коммит:  %s\n  собран:  %s\n  go:      %s\n  os/arch: %s/%s\n"),
		v.Product, v.Version, v.Author, v.Commit, v.Date, v.Go, v.OS, v.Arch)
	return err
}

// newCrewCmd is the credits: hidden, a thank-you rather than a feature.
func newCrewCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "crew",
		Hidden: true,
		Args:   cobra.NoArgs,
		Short:  i18n.T("Who made evo", "Кто сделал evo"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagJSON {
				return output.NewWithWriter(cmd.OutOrStdout(), output.ModeJSON, false).JSON(map[string]string{
					"product": product.Name, "author": i18n.T(product.AuthorEN, product.Author), "feedback": product.FeedbackContact})
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), crewText())
			return err
		},
	}
}

func crewText() string {
	return i18n.Tf(`%s
Made by %s.
Thanks to everyone who tested it and sent the output of what broke.
Feedback: %s
`, `%s
Придумал и собрал %s.
Спасибо всем, кто тестировал и присылал вывод того, что сломалось.
Обратная связь: %s
`, product.Name, i18n.T(product.AuthorEN, product.Author), product.FeedbackContact)
}
