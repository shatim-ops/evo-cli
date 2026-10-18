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
	return nil, clerrors.New(clerrors.CodeConfigInvalid,
		i18n.T("no boot image: image and image_id are not set", "не задан образ: нет ни image, ни image_id"),
		i18n.T("Set image (a name from 'evo image list') or image_id in .evo.yaml ('evo init' picks one), or pass --image.",
			"Задайте image (имя из 'evo image list') или image_id в .evo.yaml ('evo init' выберет образ сам) или передайте --image."))
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
