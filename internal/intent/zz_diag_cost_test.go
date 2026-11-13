// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// TestZZDiag_CostOfARecord is a measurement, not a check: the wall time of
// each step of the layer of intent records, per call, on the runner it
// runs on (a draft PR that is not merged; release plan 1.9, «Каталог
// записей намерений…», the threshold of internal/tools on Windows).
func TestZZDiag_CostOfARecord(t *testing.T) {
	const n = 200
	root := t.TempDir()
	per := func(name string, f func(i int)) {
		start := time.Now()
		for i := 0; i < n; i++ {
			f(i)
		}
		d := time.Since(start)
		t.Logf("DIAG %-46s %8.3f ms/op  (%d ops, %v)", name, float64(d.Microseconds())/1000/n, n, d.Round(time.Millisecond))
	}
	data := []byte(`{"tool":"vm_start","args":["name=web"]}`)
	plain := filepath.Join(root, "plain")
	_ = os.MkdirAll(plain, 0o700)
	per("os.WriteFile + os.Remove (baseline)", func(i int) {
		p := filepath.Join(plain, "f.json")
		_ = os.WriteFile(p, data, 0o600)
		_ = os.Remove(p)
	})
	per("os.CreateTemp + Rename + Remove (baseline)", func(i int) {
		f, _ := os.CreateTemp(plain, ".f.*")
		_, _ = f.Write(data)
		_ = f.Close()
		p := filepath.Join(plain, "g.json")
		_ = os.Rename(f.Name(), p)
		_ = os.Remove(p)
	})
	per("config.MkdirPrivate: a new directory", func(i int) {
		_ = config.MkdirPrivate(filepath.Join(root, "new", "d"+itoa(i)))
	})
	dir := filepath.Join(root, "intents")
	_ = config.MkdirPrivate(dir)
	per("config.MkdirPrivate: a directory that is there", func(i int) { _ = config.MkdirPrivate(dir) })
	per("config.WriteFileAtomic 0600 + os.Remove", func(i int) {
		p := filepath.Join(dir, "w.json")
		_ = config.WriteFileAtomic(p, data, 0o600)
		_ = os.Remove(p)
	})
	per("config.WriteFileAtomic 0644 + os.Remove", func(i int) {
		p := filepath.Join(dir, "x.json")
		_ = config.WriteFileAtomic(p, data, 0o644)
		_ = os.Remove(p)
	})
	in := Intent{Tool: "vm_start", Args: []string{"name=web"}}
	per("intent.Open + intent.Close (a call with consent)", func(i int) {
		rec, err := Open(dir, in)
		if err != nil {
			t.Fatal(err)
		}
		Close(rec)
	})
	per("intent.Pending: an empty directory", func(i int) { _ = Pending(dir) })
	rec, err := Open(dir, in)
	if err != nil {
		t.Fatal(err)
	}
	per("intent.Pending: one record", func(i int) { _ = Pending(dir) })
	p := recordPath(dir, rec.ID)
	per("config.ReadFileNoFollow: one record", func(i int) { _, _ = config.ReadFileNoFollow(p) })
	per("os.ReadFile: one record (baseline)", func(i int) { _, _ = os.ReadFile(p) })
	per("a fresh directory per registry: MkdirPrivate + Open + Close", func(i int) {
		d := filepath.Join(root, "reg", "r"+itoa(i), "intents")
		r, err := Open(d, in)
		if err != nil {
			t.Fatal(err)
		}
		Close(r)
	})
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{digits[i%10]}, b...)
	}
	return string(b)
}
