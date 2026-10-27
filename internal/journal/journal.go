// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package journal is the session journal of evo (D46): a local file of what
// the calls of the tools were, for a dogfood. It is off until the user
// turns it on in config.yaml (evo diag on), and it holds metadata only —
// the tool, the names of its arguments, the reason of an error, consent,
// duration and the size of the answer. No value of an argument, no answer
// and no message goes in, so there is no id, address or secret to mask.
// One file serves the whole dogfood, under a limit of its size; a full
// file says so in a record of its own. Nothing is sent anywhere: the user
// hands the file over (evo diag export).
package journal

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// Limit is the most the journal file holds over the whole dogfood. A record
// is a few hundred bytes: some fifty thousand calls.
const Limit = 16 << 20

// limit is Limit; tests lower it.
var limit int64 = Limit

// EventFull is the event of the last record of a full journal.
const EventFull = "journal_full"

const (
	fileName = "journal.jsonl"
	lockName = "journal.lock"
	// fullReserve is kept free below Limit for the record of a full
	// journal, which is shorter.
	fullReserve = 512
)

// Record is one line of the journal. The session fields (Time … OS) are
// filled by Write.
type Record struct {
	Time    string `json:"time"`
	Evo     string `json:"evo"`
	Session string `json:"session"`
	Surface string `json:"surface"`
	OS      string `json:"os"`
	// Event is set on a record that is not a call: EventFull.
	Event string `json:"event,omitempty"`
	Tool  string `json:"tool,omitempty"`
	// Args are the names of the arguments the tool's schema declares;
	// UnknownArgs counts the others (a call the check refuses), whose names
	// are the caller's text and stay out.
	Args        []string `json:"args,omitempty"`
	UnknownArgs int      `json:"unknown_args,omitempty"`
	// Depth is 1 and more for a call a tool made (a step of a stack).
	Depth     int  `json:"depth,omitempty"`
	Confirmed bool `json:"confirmed,omitempty"`
	// Preview: the answer is the preview that asks for consent.
	Preview bool `json:"preview,omitempty"`
	Error   bool `json:"error,omitempty"`
	// Reason is the code of an error (internal/errors).
	Reason string `json:"reason,omitempty"`
	MS     int64  `json:"ms"`
	Bytes  int    `json:"bytes"`
}

// The session: one process of evo, a CLI call or an MCP server.
var (
	mu      sync.Mutex
	current struct{ id, surface, version string }
)

// Surfaces of a session.
const (
	SurfaceCLI      = "cli"
	SurfaceMCPStdio = "mcp-stdio"
	SurfaceMCPHTTP  = "mcp-http"
)

// Begin starts the session of this process: the records written after it
// carry its id, surface and version of evo. Without it nothing is written.
func Begin(surface, version string) {
	var b [6]byte
	_, _ = rand.Read(b[:])
	mu.Lock()
	defer mu.Unlock()
	current.id, current.surface, current.version = hex.EncodeToString(b[:]), surface, version
}

// Path is the journal file: next to config.yaml, private as it is.
func Path() string {
	if d := config.Dir(); d != "" {
		return filepath.Join(d, fileName)
	}
	return ""
}

// Active reports whether a record would be written: a session began and
// the journal is on in config.yaml, read on each call, so turning it on
// needs no restart of an MCP server. A caller builds a record only then.
func Active() bool {
	mu.Lock()
	begun := current.id != ""
	mu.Unlock()
	return begun && config.JournalEnabled()
}

// Write appends r when a session began; the caller checked Active. A
// journal that cannot be written loses the record, never the call: the
// caller's answer does not depend on it.
func Write(r Record) {
	mu.Lock()
	defer mu.Unlock()
	if current.id == "" {
		return
	}
	r.Time = time.Now().UTC().Format(time.RFC3339)
	r.Evo, r.Session, r.Surface, r.OS = current.version, current.id, current.surface, runtime.GOOS+"/"+runtime.GOARCH
	_ = appendRecord(r)
}

// lockWait bounds the wait for another process writing a record.
var lockWait = 2 * time.Second

func appendRecord(r Record) error {
	path := Path()
	if path == "" {
		return config.NoConfigDir()
	}
	unlock, err := lock(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		if err := config.WriteFilePrivate(path, nil); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if fi.Size()+int64(len(line))+1 > limit-fullReserve {
		if full, err := endsFull(path, fi.Size()); err != nil || full {
			return err
		}
		line, _ = json.Marshal(Record{Time: r.Time, Evo: r.Evo, Session: r.Session, Surface: r.Surface, OS: r.OS, Event: EventFull})
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// endsFull reports whether the journal's last record is EventFull.
func endsFull(path string, size int64) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	off := max(size-fullReserve, 0)
	tail := make([]byte, size-off)
	if _, err := f.ReadAt(tail, off); err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return bytes.Contains(tail, []byte(`"event":"`+EventFull+`"`)), nil
}

// lock takes the OS lock of the journal in dir, waiting for another writer
// (a CLI call beside an MCP server) a little.
func lock(dir string) (unlock func(), err error) {
	if err := config.MkdirPrivate(dir); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := config.LockFile(f)
		if err == nil {
			return func() { _ = config.UnlockFile(f); _ = f.Close() }, nil
		}
		if !errors.Is(err, config.ErrLocked) || time.Now().After(deadline) {
			_ = f.Close()
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Summary describes the journal file.
type Summary struct {
	Enabled  bool   `json:"enabled"`
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	Limit    int64  `json:"limit_bytes"`
	Records  int    `json:"records"`
	Sessions int    `json:"sessions"`
	First    string `json:"first,omitempty"`
	Last     string `json:"last,omitempty"`
	// Full: the journal reached Limit and records no more.
	Full bool `json:"full"`
}

// Describe reads the journal file, under its lock: a record being written
// is not half read. No file is an empty journal.
func Describe() (Summary, error) {
	if Path() == "" {
		return Summary{Path: "", Limit: Limit}, config.NoConfigDir()
	}
	unlock, err := lock(filepath.Dir(Path()))
	if err != nil {
		return Summary{Path: Path(), Limit: Limit}, err
	}
	defer unlock()
	return describe()
}

// describe is Describe under the lock the caller holds.
func describe() (Summary, error) {
	s := Summary{Enabled: config.JournalEnabled(), Path: Path(), Limit: Limit}
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil {
		s.Bytes = fi.Size()
	}
	sessions := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		if r.Event == EventFull {
			s.Full = true
			continue
		}
		s.Records++
		sessions[r.Session] = true
		if s.First == "" {
			s.First = r.Time
		}
		s.Last = r.Time
	}
	s.Sessions = len(sessions)
	return s, sc.Err()
}

// Export copies the journal to dst (0600) and describes what it copied.
// The journal stays as it is: an export midway leaves it growing.
func Export(dst string) (Summary, error) {
	if Path() == "" {
		return Summary{}, config.NoConfigDir()
	}
	unlock, err := lock(filepath.Dir(Path()))
	if err != nil {
		return Summary{}, err
	}
	defer unlock()
	s, err := describe()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return s, ErrEmpty
	}
	if err != nil {
		return s, err
	}
	if err := config.WriteFilePrivate(dst, data); err != nil {
		return s, err
	}
	return s, nil
}

// ErrEmpty: there is no journal to export yet.
var ErrEmpty = errors.New("the journal is empty")
