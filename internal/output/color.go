// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"os"

	"golang.org/x/term"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// IsTTY returns true if the given file descriptor is a terminal.
func IsTTY(fd int) bool {
	return term.IsTerminal(fd)
}

// ColorEnabled returns true if color output should be used on stdout.
// Disabled by: NO_COLOR env var, TERM=dumb, no TTY, or explicit noColor flag.
func ColorEnabled(noColor bool) bool {
	return colorAllowed(noColor) && IsTTY(int(os.Stdout.Fd()))
}

// colorAllowed applies the flag and the environment, not the terminal.
// NO_COLOR counts only when non-empty (https://no-color.org).
func colorAllowed(noColor bool) bool {
	return !noColor && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}
