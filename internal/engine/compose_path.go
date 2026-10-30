// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"path/filepath"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// composePath is the absolute path of a compose file, as a deployment
// records it (state.DeploymentRecord.ComposePath).
func composePath(file string) string {
	if abs, err := filepath.Abs(file); err == nil {
		return abs
	}
	return filepath.Clean(file)
}

// composeRefusal refuses, before the question, a deploy of compose file
// file to the VM of rec when rec runs another one (D62): an MCP server
// started in one directory, an agent working on another project, and the
// compose file of one sent to the VM of the other. nil when rec runs file,
// or records no path (a record from before 1.6: composeUnknown warns).
func composeRefusal(rec *state.DeploymentRecord, file string) error {
	if rec == nil || rec.ComposePath == "" || rec.ComposePath == composePath(file) {
		return nil
	}
	return clerrors.New(clerrors.CodeUsage,
		i18n.Tf("deployment %s runs %s, not %s: deploy would send another project's compose file to its VM; nothing was done",
			"развёртывание %s работает с %s, а не с %s: deploy отправил бы на его ВМ compose-файл другого проекта; ничего не сделано",
			rec.ID, rec.ComposePath, composePath(file)),
		i18n.Tf("Deploy from the directory of %s (for evo mcp: start the server there), or pass that file as compose_file.",
			"Запустите deploy из каталога %s (для evo mcp — запустите сервер там) или передайте этот файл в compose_file.", rec.ComposePath))
}

// composeUnknown is the warning of a deploy that updates the VM of rec
// whose record names no compose file (from before 1.6): which project it
// runs is not known, and this deploy sends file to it.
func composeUnknown(rec *state.DeploymentRecord, file string) string {
	if rec == nil || rec.ComposePath != "" {
		return ""
	}
	return i18n.Tf("deployment %s does not record its compose file (made before evo 1.6): this deploy sends %s to its VM and records it",
		"развёртывание %s не записало свой compose-файл (создано до evo 1.6): этот deploy отправит на его ВМ %s и запишет его",
		rec.ID, composePath(file))
}

// DeployComposeCheck is composeRefusal and composeUnknown of the deployment
// this directory's deploy would update, for the deploy tool's answer
// before the question: the refusal, else the warning ("" without one).
func (e *Engine) DeployComposeCheck() (warning string, err error) {
	sf, rerr := state.Read(state.DefaultPath(e.StateDir))
	if rerr != nil {
		return "", nil
	}
	rec := sf.ActiveComposeDeployment()
	if err := composeRefusal(rec, e.Config.ComposeFile); err != nil {
		return "", err
	}
	return composeUnknown(rec, e.Config.ComposeFile), nil
}
