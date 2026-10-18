// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors

import "fmt"

// CLIError is a structured error carrying a machine-readable code,
// a human-readable message, an actionable hint, and an optional wrapped cause.
type CLIError struct {
	Code    string
	Message string
	Hint    string
	Err     error // wrapped original error
}

// New creates a CLIError without a wrapped cause.
func New(code, message, hint string) *CLIError {
	return &CLIError{
		Code:    code,
		Message: message,
		Hint:    hint,
	}
}

// Wrap creates a CLIError that wraps an underlying error.
func Wrap(code, message, hint string, err error) *CLIError {
	return &CLIError{
		Code:    code,
		Message: message,
		Hint:    hint,
		Err:     err,
	}
}

// Error returns the string representation in the form "CODE: message".
func (e *CLIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the wrapped error, enabling errors.Unwrap / errors.Is chains.
func (e *CLIError) Unwrap() error {
	return e.Err
}

// Is reports whether target is a *CLIError with the same Code.
// This allows errors.Is to match CLIErrors by their error code.
func (e *CLIError) Is(target error) bool {
	t, ok := target.(*CLIError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}
