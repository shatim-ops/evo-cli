// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"os"
	"testing"
)

// livecatalogue_test.go is one helper in three packages of tests (cli,
// tools, engine): the copies differ only in their package line, or a world
// of one package would be held to another rule than the rest.
func TestLiveCatalogue_CopiesSame(t *testing.T) {
	var first []byte
	for _, path := range []string{"../cli/livecatalogue_test.go", "../tools/livecatalogue_test.go", "livecatalogue_test.go"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		i := bytes.Index(b, []byte("\npackage "))
		j := bytes.IndexByte(b[i+1:], '\n')
		body := append(append([]byte{}, b[:i+1]...), b[i+1+j:]...)
		if first == nil {
			first = body
			continue
		}
		if !bytes.Equal(body, first) {
			t.Errorf("%s differs from internal/cli/livecatalogue_test.go (besides the package line): keep the copies the same", path)
		}
	}
}
