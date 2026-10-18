// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The default 10 GB disk is below ubuntu-22.04's min_disk (11 GB since
// 15.10.2026): the create API answers 422 vm_root_disk_too_small. Deploy
// asks for the image minimum and says so.
func TestDeploy_BootDiskRaisedToImageMinimum(t *testing.T) {
	eng, cloud, opts, _, stderr := newDeployForConfirm(t)
	withPromptStdin(t, strings.NewReader(""), false)
	opts.Yes = true
	img, _ := (&mockCloudAPI{}).GetImage(context.Background(), "img")
	img.MinDisk = 11
	cloud.getImageFn = func(context.Context, string) (*provision.ImageDetail, error) { return img, nil }

	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("deploy --yes: %v", err)
	}
	if got := cloud.createReq.Disks[0].Size; got != 11 {
		t.Fatalf("boot disk %d GB, want the image minimum 11", got)
	}
	if !strings.Contains(stderr.String(), "boot disk raised from 10 to 11 GB") {
		t.Errorf("no warning about the raised disk:\n%s", stderr.String())
	}
}
