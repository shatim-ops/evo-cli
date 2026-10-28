// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"fmt"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// BootImage reads the configured boot image with its metadata fields:
// image_id by UUID as it is (one read and the project's check), image by a
// catalogue reference — an exact name, a display name or a UUID
// (images.Resolve: ambiguous is USAGE with the candidates, none is
// NOT_FOUND). Neither set is CONFIG_INVALID. An image of the shared
// catalogue the project cannot use is NOT_FOUND here, before anything is
// created: the create call would answer 422 wrong_image
// (images.NotInProjectError).
func (e *Engine) BootImage(ctx context.Context, projectID string) (*provision.ImageDetail, error) {
	cat := images.Catalog{Cloud: e.Cloud, ProjectID: projectID}
	var img *provision.ImageDetail
	switch {
	case e.Config.ImageID != "":
		d, err := e.Cloud.GetImage(ctx, e.Config.ImageID)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("get image details: %w", "сведения об образе: %w"), err)
		}
		cat.MarkInProject(ctx, d)
		img = d
	case e.Config.Image != "":
		d, err := cat.Get(ctx, e.Config.Image)
		if err != nil {
			return nil, err
		}
		img = d
	}
	if img != nil {
		if img.NotInProject {
			return nil, images.NotInProjectError(img.Name)
		}
		return img, nil
	}
	return nil, errNoBootImage()
}

func errNoBootImage() error {
	return &MissingSettingError{Arg: "image", ce: clerrors.New(clerrors.CodeConfigInvalid,
		i18n.T("no boot image: image and image_id are not set", "не задан образ: нет ни image, ни image_id"),
		i18n.T("Set image (a name from 'evo image list') or image_id in .evo.yaml ('evo init' picks one), or pass --image.",
			"Задайте image (имя из 'evo image list') или image_id в .evo.yaml ('evo init' выберет образ сам) или передайте --image."))}
}

func errNoZone() error {
	return &MissingSettingError{Arg: "region", ce: clerrors.New(clerrors.CodeConfigInvalid,
		i18n.T("no availability zone is set (region)", "не задана зона доступности (region)"),
		i18n.T("Set region in .evo.yaml or the profile (evo configure --region <zone>), or pass --region: a zone name such as ru.AZ-2 or its UUID (evo vm zones).",
			"Задайте region в .evo.yaml или в профиле (evo configure --region <зона>) или передайте --region — имя зоны (например, ru.AZ-2) или её UUID (evo vm zones)."))}
}

// MissingSettingError is a setting a new VM of deploy cannot do without,
// set nowhere: the call's argument, .evo.yaml, the profile and the
// environment all leave it empty. Arg is the tool argument that sets it,
// for the "missing" of the answer.
type MissingSettingError struct {
	Arg string
	ce  *clerrors.CLIError
}

func (e *MissingSettingError) Error() string { return e.ce.Error() }
func (e *MissingSettingError) Unwrap() error { return e.ce }

// MissingSetting is the argument a MissingSettingError names, "" for any
// other error.
func MissingSetting(err error) string {
	var e *MissingSettingError
	if stderrors.As(err, &e) {
		return e.Arg
	}
	return ""
}

// DeployInputError refuses a new VM of deploy without an image or a zone.
// Neither is read from the cloud: the call fails whatever the person
// answers, so it is refused before the question, on every surface — the
// preview of the tool, its confirmed call and evo deploy alike (D48, the
// rule of D15 for provision). The zone is never picked silently (D15).
func (e *Engine) DeployInputError() error {
	switch {
	case e.ImageRef() == "":
		return errNoBootImage()
	case e.Config.Region == "":
		return errNoZone()
	}
	return nil
}

// ImageRef is the configured boot image as a person gave it: image_id or
// image, "" when neither is set.
func (e *Engine) ImageRef() string {
	return firstNonEmpty(e.Config.ImageID, e.Config.Image)
}

// inProjectFile makes a refused image_metadata value (USAGE from
// images.FillMetadata) a project-file error: deploy reads the values from
// .evo.yaml, so that is what to fix.
func inProjectFile(err error) error {
	var ce *clerrors.CLIError
	if stderrors.As(err, &ce) && ce.Code == clerrors.CodeUsage {
		return clerrors.New(clerrors.CodeConfigInvalid, "image_metadata (.evo.yaml): "+ce.Message, ce.Hint)
	}
	return err
}
