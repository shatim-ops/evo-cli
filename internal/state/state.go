// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierr "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// CurrentVersion is the state file format this build reads and writes.
const CurrentVersion = 1

// DefaultPath returns ".evo/state.json" relative to the given project root,
// or the legacy ".cloudru-vm/state.json" of a project deployed before the
// rename (see config.DataDir).
func DefaultPath(root string) string {
	return filepath.Join(config.DataDir(root), "state.json")
}

// Read reads the state file. Returns empty File (version=1) if file doesn't exist.
// Returns CLIError(STATE_CORRUPTED) if JSON is invalid.
func Read(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{
				Version:     CurrentVersion,
				Deployments: []DeploymentRecord{},
			}, nil
		}
		return nil, clierr.Wrap(
			clierr.CodeStateCorrupted,
			i18n.T("failed to read state file", "не удалось прочитать файл состояния"),
			i18n.T("Check file permissions for ", "Проверьте права на файл ")+path,
			err,
		)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		// Deleting the file forgets VMs and floating IPs that keep being
		// billed: say so before suggesting it.
		return nil, clierr.Wrap(
			clierr.CodeStateCorrupted,
			i18n.T("state file contains invalid JSON", "в файле состояния ошибка JSON"),
			i18n.Tf("Fix the JSON in %s. Before deleting it, note the VMs and floating IPs it lists "+
				"(they keep running and are billed) or check them in the Cloud.ru console.",
				"Исправьте JSON в %s. Прежде чем удалять файл, выпишите ВМ и floating IP из него "+
					"(они продолжают работать и оплачиваются) или найдите их в консоли Cloud.ru.", path),
			err,
		)
	}

	switch {
	case f.Version == 0: // written before the field existed
		f.Version = CurrentVersion
	case f.Version > CurrentVersion:
		// Rewriting it would drop what this version does not know.
		return nil, clierr.New(
			clierr.CodeStateCorrupted,
			i18n.Tf("state file %s has version %d; this evo understands up to %d",
				"файл состояния %s версии %d, а этот evo понимает версии до %d", path, f.Version, CurrentVersion),
			i18n.T("Upgrade evo; the newer version wrote this state.", "Обновите evo: этот файл записала более новая версия."),
		)
	}

	if f.Deployments == nil {
		f.Deployments = []DeploymentRecord{}
	}

	return &f, nil
}

// Write atomically writes the state file (write .tmp, rename).
// Creates parent directory if needed.
func Write(path string, f *File) error {
	dir := filepath.Dir(path)
	if err := config.MkdirPrivate(dir); err != nil { // .evo/ also holds the SSH key
		return clierr.Wrap(
			clierr.CodeStateCorrupted,
			stateDirFailed(),
			checkWritable(dir),
			err,
		)
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return clierr.Wrap(
			clierr.CodeStateCorrupted,
			i18n.T("failed to marshal state", "не удалось сериализовать состояние"),
			i18n.T("This is a bug; please report it", "Это ошибка evo, сообщите о ней"),
			err,
		)
	}

	// A temporary file of its own for every writer: with one fixed name two
	// concurrent writers renamed each other's file away. Synced before the
	// rename, so a crash leaves the old state or the new, never an empty file.
	tmpFile, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return clierr.Wrap(
			clierr.CodeStateCorrupted,
			tmpWriteFailed(),
			checkSpace(dir),
			err,
		)
	}
	tmp := tmpFile.Name()
	_, err = tmpFile.Write(data)
	if err == nil {
		err = tmpFile.Sync()
	}
	if cerr := tmpFile.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o644) // CreateTemp makes it 0600
	}
	if err != nil {
		_ = os.Remove(tmp)
		return clierr.Wrap(
			clierr.CodeStateCorrupted,
			tmpWriteFailed(),
			checkSpace(dir),
			err,
		)
	}

	if err := os.Rename(tmp, path); err != nil {
		// Clean up tmp file on rename failure.
		_ = os.Remove(tmp)
		return clierr.Wrap(
			clierr.CodeStateCorrupted,
			i18n.T("failed to atomically replace state file", "не удалось атомарно заменить файл состояния"),
			i18n.T("Check permissions for ", "Проверьте права на ")+path,
			err,
		)
	}

	return nil
}

func stateDirFailed() string {
	return i18n.T("failed to create state directory", "не удалось создать каталог состояния")
}

func checkWritable(dir string) string {
	return i18n.T("Check write permissions for ", "Проверьте права на запись в ") + dir
}

func tmpWriteFailed() string {
	return i18n.T("failed to write temporary state file", "не удалось записать временный файл состояния")
}

func checkSpace(dir string) string {
	return i18n.T("Check disk space and permissions for ", "Проверьте свободное место и права на ") + dir
}

// FindDeployment returns the first deployment with the given ID, or nil.
func (f *File) FindDeployment(id string) *DeploymentRecord {
	for i := range f.Deployments {
		if f.Deployments[i].ID == id {
			return &f.Deployments[i]
		}
	}
	return nil
}

// ActiveDeployment returns the first deployment with status != "destroyed", or nil.
func (f *File) ActiveDeployment() *DeploymentRecord {
	for i := range f.Deployments {
		if f.Deployments[i].Status != "destroyed" {
			return &f.Deployments[i]
		}
	}
	return nil
}

// UpsertDeployment adds or updates a deployment record by ID.
func (f *File) UpsertDeployment(rec DeploymentRecord) {
	for i := range f.Deployments {
		if f.Deployments[i].ID == rec.ID {
			f.Deployments[i] = rec
			return
		}
	}
	f.Deployments = append(f.Deployments, rec)
}

// RemoveDeployment removes a deployment by ID.
func (f *File) RemoveDeployment(id string) {
	n := 0
	for _, d := range f.Deployments {
		if d.ID != id {
			f.Deployments[n] = d
			n++
		}
	}
	f.Deployments = f.Deployments[:n]
}
