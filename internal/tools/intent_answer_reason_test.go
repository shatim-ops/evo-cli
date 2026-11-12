// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "testing"

// The reason of a tool's error answer, whose "error" is a string, and of the
// CLI's --json one, whose "error" is an object: a struct of both read a
// tool's error as one with no reason, and the record of a call whose answer
// was lost was closed as if its outcome were known (the audit of #1544, Б2).
func TestAnswerReason_BothForms(t *testing.T) {
	for answer, want := range map[string]string{
		`{"status":"error","error":"create backup: API_ERROR: 502","reason":"API_ERROR"}`: "API_ERROR",
		`{"error":{"code":5,"reason":"API_ERROR","message":"x"}}`:                         "API_ERROR",
		`{"status":"created"}`: "",
		`not json`:             "",
	} {
		if got := answerReason(answer); got != want {
			t.Errorf("answerReason(%s) = %q, want %q", answer, got, want)
		}
	}
}
