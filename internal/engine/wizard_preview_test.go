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

// Plan 1.8, «Решения владельца 10.11 по заключению 36», (2): a Marketplace
// image opens its installation wizard on 80/443 — open to anyone, whoever
// comes first finishes it. The preview says so before the question and
// names cidr; with a cidr, or with an image of its own, there is no such
// line. The order of AddPorts and the image's type does not matter.
func TestPreview_MarketplaceWizardOpenToAnyone(t *testing.T) {
	for _, c := range []struct {
		name        string
		marketplace bool
		cidr        string
		ports       []int
		warn        bool
	}{
		{"marketplace, 80 to anyone", true, "", []int{80}, true},
		{"marketplace, 443 to anyone", true, "", []int{22, 443}, true},
		{"marketplace, 80 to the person", true, "203.0.113.7/32", []int{80}, false},
		{"marketplace, no web port", true, "", []int{8080}, false},
		{"own image, 80 to anyone", false, "", []int{80}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			cloud := &mockCloudAPI{getImageFn: func(_ context.Context, id string) (*provision.ImageDetail, error) {
				img := &provision.ImageDetail{ID: id, Name: "wordpress"}
				if c.marketplace {
					img.Type = provision.ImageTypeMarketplace
				}
				return img, nil
			}}
			e := &Engine{Config: config.Config{ProjectID: "p"}, Cloud: cloud,
				Auth: &mockTokenProvider{token: auth.Token{Value: "t", ExpiresAt: time.Now().Add(time.Hour)}}}
			p := CreatePreview{Resource: ResourceVM, Name: "web", ProjectID: "p",
				VM: &deployplan.VMSpec{Image: deployplan.RefSpec{ID: "00000000-0000-0000-0000-0000000000a1"}, Disk: deployplan.DiskSpec{SizeGB: 10}}}
			p.AddPorts(c.ports, nil, c.cidr)
			if err := e.CompletePreview(context.Background(), &p); err != nil {
				t.Fatal(err)
			}
			got := strings.Contains(strings.Join(p.Warnings, "\n"), WizardOpen())
			if got != c.warn {
				t.Errorf("wizard warning %v, want %v: %q", got, c.warn, p.Warnings)
			}
			if n := strings.Count(strings.Join(p.Warnings, "\n"), WizardOpen()); n > 1 {
				t.Errorf("said %d times", n)
			}
		})
	}
}
