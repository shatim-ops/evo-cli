// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "testing"

// Full audit 1.5, FB150-1: the image of ca_deploy is a container image of
// Artifact Registry, not a VM image; Tab offered image_list (Ubuntu-24.04)
// for it. The image of a VM (provision, deploy) stays completed.
func TestCompletion_ContainerImageIsNotAVMImage(t *testing.T) {
	r := New(nil)
	if c, ok := r.completionOf("ca_deploy", "image"); ok {
		t.Errorf("ca_deploy image is completed from %s: a VM image list for a container image", c.list)
	}
	for _, tool := range []string{"provision", "deploy"} {
		if c, ok := r.completionOf(tool, "image"); !ok || c.list != "image_list" {
			t.Errorf("%s image: completed=%v from %q, want image_list", tool, ok, c.list)
		}
	}
}
