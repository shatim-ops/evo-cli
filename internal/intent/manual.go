// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// ManualSnapshot keeps the preview's descriptor open across the question.
// An old evo replaces the file by rename: equal bytes alone cannot identify it.
type ManualSnapshot struct {
	Record  Intent
	source  *os.File
	digest  string
	profile string
	audit   string
}

func (s *ManualSnapshot) Close() { s.source.Close() }

func manualError(en, ru string) error { return errors.New(i18n.T(en, ru)) }

func sameObject(a, b *os.File) bool {
	left, e := a.Stat()
	right, f := b.Stat()
	return e == nil && f == nil && os.SameFile(left, right)
}

func manualDirectory(path string, private bool) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() || (private && (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0 || config.PrivateDirWarning(path) != "")) {
		return manualError("Unsafe directory for manual decisions.", "Небезопасный каталог для ручных решений.")
	}
	return nil
}

// Trust a user-selected configuration directory only if other users cannot
// replace its private decisions directory through the parent.
func trustedConfigDirectory() error {
	path := config.Dir()
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm()&0022 != 0 || config.PrivateDirWarning(path) != "" {
		return manualError("The configuration directory is not safe for manual decisions.", "Каталог конфигурации не защищён от подмены ручных решений.")
	}
	return nil
}

// PreviewManual does no reconciliation and makes no writes. ConfigDir receipts
// and ambiguous IDs are deliberately outside the supported scope.
func PreviewManual(root, id, profile string) (*ManualSnapshot, error) {
	if !validID(id) || profile == "" || config.Dir() == "" {
		return nil, manualError("Expected a full receipt ID and an active profile.", "Требуются полный ID квитанции и активный профиль.")
	}
	if err := trustedConfigDirectory(); err != nil {
		return nil, err
	}
	dir := Dir(root)
	if candidate := ConfigDir(); candidate != "" {
		if candidate == dir {
			return nil, manualError("Receipt scope is ambiguous.", "Неоднозначная область квитанции.")
		}
		if err := manualDirectory(candidate, false); err == nil {
			if _, err = os.Lstat(recordPath(candidate, id)); !os.IsNotExist(err) {
				return nil, manualError("A configuration receipt or duplicate ID cannot be closed here.", "Квитанцию настроек или неоднозначный ID здесь закрыть нельзя.")
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	for _, d := range []string{filepath.Dir(dir), dir} {
		if err := manualDirectory(d, false); err != nil {
			return nil, err
		}
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	f, err := openManualReceipt(recordPath(dir, id))
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	var in Intent
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if !json.Valid(data) || dec.Decode(&in) != nil || in.ID != id || in.Tool == "" || in.StartedAt.IsZero() {
		f.Close()
		return nil, manualError("Unrecognized receipt format.", "Нераспознанный формат квитанции.")
	}
	in.Dir = dir
	hash := sha256.Sum256(data)
	bucket := sha256.Sum256([]byte(profile + "\x00" + dir + "\x00" + id))
	return &ManualSnapshot{Record: in, source: f, digest: hex.EncodeToString(hash[:]), profile: profile, audit: filepath.Join(config.Dir(), "intent-decisions", hex.EncodeToString(bucket[:]))}, nil
}

// Eligible returns an error if the known owner, project or lifecycle does not
// allow a human to dismiss this receipt. Tool-specific scope is checked by the caller.
func (s *ManualSnapshot) Eligible(key *state.Maker, project string) error {
	in := s.Record
	if !in.Key.Matches(key) || project == "" || in.ProjectID != project {
		return manualError("Cannot verify the owner and project of this receipt.", "Не удалось подтвердить владельца и проект квитанции.")
	}
	if in.Held || in.InFlight() || !in.Ended && (in.Machine == "" || in.Machine != thisMachine() || in.PID <= 0) || !in.Sent || !in.Counts {
		return manualError("The receipt may be active, held or of an unsupported format.", "Квитанция может быть активной, удержанной или неподдерживаемого формата.")
	}
	return nil
}

type manualDecision struct {
	Format    int       `json:"format"`
	ID        string    `json:"intent_id"`
	Profile   string    `json:"profile"`
	Directory string    `json:"directory"`
	Tool      string    `json:"tool"`
	Project   string    `json:"project_id"`
	Digest    string    `json:"sha256"`
	Decision  string    `json:"decision"`
	Outcome   string    `json:"cloud_outcome"`
	Method    string    `json:"confirmation"`
	At        time.Time `json:"time"`
}

func (s *ManualSnapshot) decision(at time.Time) manualDecision {
	return manualDecision{1, s.Record.ID, s.profile, s.Record.Dir, s.Record.Tool, s.Record.ProjectID, s.digest, "closed_locally", "unknown", "terminal_full_id", at}
}

func HasManualDecisions() bool {
	dir := config.Dir()
	return dir != "" && manualDirectory(filepath.Join(dir, "intent-decisions"), true) == nil
}

// Marked checks the decision AND its anchor. The caller keeps the receipt
// visible on every read error. Pending and automatic write guards never use this.
func (s *ManualSnapshot) Marked() (bool, error) {
	for _, d := range []string{filepath.Dir(s.audit), s.audit} {
		if err := manualDirectory(d, true); err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
	}
	entries, err := os.ReadDir(s.audit)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "decision-") {
			continue
		}
		dir := filepath.Join(s.audit, e.Name())
		if err := manualDirectory(dir, true); err != nil {
			return false, err
		}
		anchor, err := config.OpenNoFollow(filepath.Join(dir, "receipt"), os.O_RDONLY, 0)
		if err != nil {
			return false, err
		}
		same := sameObject(s.source, anchor)
		anchor.Close()
		if !same {
			continue
		}
		data, err := config.ReadFileNoFollow(filepath.Join(dir, "decision.json"))
		if err != nil {
			return false, err
		}
		var d manualDecision
		if json.Unmarshal(data, &d) != nil || d.At.IsZero() || d != s.decision(d.At) {
			return false, manualError("The manual decision could not be verified.", "Не удалось проверить ручное решение.")
		}
		if err := syncDirectory(s.audit); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// Apply reopens the authorized scope under a lock shared by new decisions.
// Replacements by older clients invalidate the anchor without any cooperation.
func (s *ManualSnapshot) Apply(recheck func() (*ManualSnapshot, error)) error {
	if err := trustedConfigDirectory(); err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(s.audit), s.audit} {
		if err := config.MkdirPrivate(dir); err != nil {
			return err
		}
		if err := manualDirectory(dir, true); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(dir)); err != nil {
			return err
		}
	}
	unlock, err := state.FileLock(filepath.Join(s.audit, "lock"), time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	check := func() error {
		fresh, err := recheck()
		if err != nil {
			return err
		}
		defer fresh.Close()
		if s.audit != fresh.audit || s.digest != fresh.digest || !sameObject(s.source, fresh.source) {
			return manualError("Receipt changed. Review it again.", "Квитанция изменилась. Проверьте её заново.")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	marked, err := s.Marked()
	if err != nil {
		return err
	}
	if marked {
		return syncDirectory(s.audit)
	}
	stage, err := os.MkdirTemp(s.audit, ".prepare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	anchor := filepath.Join(stage, "receipt")
	if err := os.Link(s.source.Name(), anchor); err != nil {
		return fmt.Errorf("%s: %w", i18n.T("cannot anchor this receipt in the local configuration directory", "не удалось закрепить эту квитанцию в локальном каталоге настроек"), err)
	}
	a, err := config.OpenNoFollow(anchor, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	same := sameObject(s.source, a)
	a.Close()
	if !same {
		return manualError("Receipt replaced.", "Квитанция заменена.")
	}
	data, err := json.Marshal(s.decision(time.Now().UTC()))
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(stage, "decision.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := syncDirectory(stage); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	final := filepath.Join(s.audit, "decision-"+filepath.Base(stage)[len(".prepare-"):])
	if err := publishManualDirectory(stage, final); err != nil {
		return err
	}
	if err := syncDirectory(s.audit); err != nil {
		return err
	}
	return check()
}
