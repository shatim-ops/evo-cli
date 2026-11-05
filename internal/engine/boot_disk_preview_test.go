// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Agent run of 1.7.0 (c1, c0, m0; findings 2.1, №15): a disk_size below the
// image's minimum showed 11 GB in the preview's VM and said nothing, while
// the create warned «boot disk raised from 5 to 11 GB». The preview says
// it before the question, with the same words.
func TestProvisionPreview_SaysTheBootDiskIsRaised(t *testing.T) {
	cloud := &mockCloudAPI{getImageFn: func(_ context.Context, id string) (*provision.ImageDetail, error) {
		return &provision.ImageDetail{ID: id, Name: "ubuntu", MinDisk: 11}, nil
	}}
	e := &Engine{Config: config.Config{ProjectID: "p"}, Cloud: cloud,
		Auth: &mockTokenProvider{token: auth.Token{Value: "t", ExpiresAt: time.Now().Add(time.Hour)}}}
	p := CreatePreview{Resource: ResourceVM, Name: "web", ProjectID: "p",
		VM: &deployplan.VMSpec{Image: deployplan.RefSpec{ID: "00000000-0000-0000-0000-0000000000a1"}, Disk: deployplan.DiskSpec{SizeGB: 5}}}
	if err := e.CompletePreview(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	_, want := provision.BootDiskGB(5, 11)
	if p.VM.Disk.SizeGB != 11 {
		t.Fatalf("boot disk %d GB, want the image's 11", p.VM.Disk.SizeGB)
	}
	if !strings.Contains(strings.Join(p.Warnings, "\n"), want) {
		t.Errorf("the preview raises the boot disk to 11 GB and says nothing: warnings %q, want %q", p.Warnings, want)
	}
}
