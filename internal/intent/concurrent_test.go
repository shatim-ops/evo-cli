// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"os"
	"sync"
	"testing"
	"time"
)

// Many calls of one process write, mark, read and close records in one
// directory at once — two durable writes per call (the record, then its
// «sent» mark) and a scan that reads every file. On Windows a rename over
// a record another goroutine holds open for reading is refused until it
// closes; the write is retried (config.renameAtomic), so none is lost and
// no call fails to record what it is about to do (the Windows run of
// TestQA11_ToolErrorsSpeakCLILanguage under -race; conclusion 187, У2).
func TestConcurrent_WriteMarkReadCloseInOneDirectory(t *testing.T) {
	dir := t.TempDir()
	const workers, each = 16, 12
	var wg sync.WaitGroup
	errs := make(chan error, workers*each)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				in, err := Open(dir, Intent{Tool: "vm_reboot", Target: "vm_id=x", PID: os.Getpid(), Counts: true, StartedAt: time.Now()})
				if err != nil {
					errs <- err
					return
				}
				if err := MarkSent(in); err != nil { // the second durable write
					errs <- err
					return
				}
				_ = Pending(dir) // a scan that reads every record
				Close(in)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if left := Pending(dir); len(left) != 0 {
		t.Errorf("%d records left of calls that all closed", len(left))
	}
}
