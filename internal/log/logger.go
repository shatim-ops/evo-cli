// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"io"
	"log/slog"
)

// Logger wraps slog.Logger with credential sanitization.
type Logger struct {
	*slog.Logger
}

// New creates a Logger writing to w. If verbose is true, level is Debug; otherwise Info.
func New(w io.Writer, verbose bool) *Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	sanitized := &sanitizeWriter{w: w}
	handler := slog.NewTextHandler(sanitized, &slog.HandlerOptions{Level: level})
	return &Logger{Logger: slog.New(handler)}
}
