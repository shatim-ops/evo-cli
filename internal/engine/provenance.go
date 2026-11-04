// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The answers of Provenance.CreatedByEvo.
const (
	ProvenanceYes     = "yes"
	ProvenanceNo      = "no"
	ProvenanceUnknown = "unknown"
)

// The kinds of resource a folder's state records, for ProvenanceOf: any
// other kind is not recorded (until the journal, D43).
const (
	ProvenanceVM         = "vm"
	ProvenanceFloatingIP = "floating_ip"
)

// Provenance is the "provenance" of a delete preview (release plan 1.7,
// P3): whether this folder's evo made the resource, and why it says so. It
// is information for the person and the agent, not a boundary — the
// boundary is the key's rights in IAM (owner's decision 03.11) — and it
// changes nothing in what the delete does.
type Provenance struct {
	CreatedByEvo string `json:"created_by_evo"`
	Note         string `json:"note"`
}

// recordedID are the fields of a record that name a resource of a kind the
// state records.
var recordedID = map[string]func(state.DeploymentRecord) string{
	ProvenanceVM:         func(r state.DeploymentRecord) string { return r.VMID },
	ProvenanceFloatingIP: func(r state.DeploymentRecord) string { return r.FloatingIPID },
}

// ProvenanceOf is the one point that says whether this folder's evo made
// the resource id of kind (ProvenanceVM, ProvenanceFloatingIP, or any other
// kind, which the state does not record), made by the cloud at made (zero:
// the cloud gives no time), by the folder's state sf as state.Read gave it
// with readErr. madeErr is the error of the cloud's read that gives made,
// when the preview went on without it. yes: a record names the id and the
// resource is not older than it, by the rule of AdoptedVM (a time missing
// on either side is taken as own, as AdoptedVM takes it); no: a record
// names it, but the resource was made before it (an evo before 1.7 adopted
// it by its name, D73); unknown: the kind is not recorded, no record names
// it, the state could not be read, or the time of a recorded resource
// could not be read — each with its own note, and none fails the preview.
func ProvenanceOf(kind, id string, made time.Time, madeErr error, sf *state.File, readErr error) Provenance {
	unknown := func(note string) Provenance { return Provenance{CreatedByEvo: ProvenanceUnknown, Note: note} }
	idOf, recorded := recordedID[kind]
	switch {
	case !recorded:
		return unknown(i18n.T("evo does not record this kind of resource until the journal (D43, 1.8): whether evo made it is unknown",
			"evo не записывает такие ресурсы до журнала (D43, 1.8): создан ли он evo — неизвестно"))
	case readErr != nil:
		return unknown(i18n.Tf("this folder's state could not be read (%s): evo cannot tell who made it",
			"state этой папки не прочитан (%s): кто создал ресурс, evo сказать не может", clerrors.Reason(readErr)))
	}
	var own, adopted *state.DeploymentRecord
	if sf != nil && id != "" {
		for i := range sf.Deployments {
			rec := &sf.Deployments[i]
			if idOf(*rec) != id {
				continue
			}
			if madeBeforeRecord(*rec, made) {
				adopted = rec
			} else if own == nil {
				own = rec
			}
		}
	}
	switch {
	case (adopted != nil || own != nil) && madeErr != nil:
		return unknown(i18n.Tf("this folder's state records it, but the cloud's time it was made could not be read (%s): whether evo made it is unknown",
			"state этой папки его записывает, но время создания из облака не прочитано (%s): создан ли он evo — неизвестно", clerrors.Reason(madeErr)))
	case adopted != nil:
		// Of several records, one of an adoption decides: destroy refuses
		// it all the same (AdoptedVM).
		return Provenance{CreatedByEvo: ProvenanceNo, Note: i18n.Tf(
			"this folder's state records it (deployment %s), but the cloud made it before the record: an evo before 1.7 adopted it by its name, it did not create it",
			"state этой папки его записывает (развёртывание %s), но облако создало его раньше записи: evo до 1.7 взял его по имени, а не создал", adopted.ID)}
	case own != nil:
		return Provenance{CreatedByEvo: ProvenanceYes, Note: i18n.Tf(
			"this folder's state records it (deployment %s), made no earlier than the record",
			"state этой папки его записывает (развёртывание %s), создан не раньше записи", own.ID)}
	}
	return unknown(i18n.T("this folder's state does not record it: it is another folder's or made outside evo (the console, another tool), whether evo made it is unknown",
		"state этой папки его не записывает: он другой папки или создан не через evo (консоль, другой инструмент), создан ли он evo — неизвестно"))
}

// madeBeforeRecord is the rule of D73: the resource, made by the cloud at
// made, is older than the record rec beyond the clocks' skew. A time
// missing on either side says nothing: not before.
func madeBeforeRecord(rec state.DeploymentRecord, made time.Time) bool {
	return !rec.CreatedAt.IsZero() && !made.IsZero() && !provision.MadeSince(&provision.VM{CreatedAt: made}, rec.CreatedAt)
}

// ProvenanceLine is the line of a human preview for p.
func ProvenanceLine(p Provenance) string {
	value := p.CreatedByEvo
	switch p.CreatedByEvo {
	case ProvenanceYes:
		value = i18n.T("yes", "да")
	case ProvenanceNo:
		value = i18n.T("no", "нет")
	case ProvenanceUnknown:
		value = i18n.T("unknown", "неизвестно")
	}
	return i18n.T("created by evo: ", "создан evo: ") + value + " — " + p.Note
}
