// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"regexp"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// noDockerOS are the OS families get.docker.com does not install Docker on
// (it knows Ubuntu, Debian, CentOS, Fedora, RHEL…), by a word of the image
// name, display name or os field. The domestic distributions are real VM
// targets, so only deploy refuses them; evo vm create takes them.
var noDockerOS = []struct {
	family string
	re     *regexp.Regexp
}{
	{"Astra Linux", regexp.MustCompile(`(?i)astra`)},
	{"Alt Linux", regexp.MustCompile(`(?i)(^|[^a-z])alt([^a-z]|$)|altlinux|(^|[^а-яё])альт([^а-яё]|$)`)},
	{"RED OS", regexp.MustCompile(`(?i)(^|[^a-z])red[ _-]?os([^a-z]|$)|(^|[^а-яё])ред[ _-]?ос([^а-яё]|$)`)},
}

// NoDockerFamily names the OS family of an image whose Docker deploy
// cannot install ("Astra Linux", "Alt Linux", "RED OS"), or "" for any
// other image. The fields are the image's name, display name and os.
func NoDockerFamily(fields ...string) string {
	for _, f := range fields {
		for _, x := range noDockerOS {
			if x.re.MatchString(f) {
				return x.family
			}
		}
	}
	return ""
}

// NoDockerError refuses a deploy onto an image of such a family, before
// anything is created. code is CONFIG_INVALID for the project file, USAGE
// for an argument.
func NoDockerError(code, image, family string) error {
	return clierrors.New(code,
		i18n.Tf("deploy does not support image %q: Docker's install script (get.docker.com) does not work on %s",
			"deploy не работает с образом %q: скрипт установки Docker (get.docker.com) не поддерживает %s", image, family),
		i18n.T("Take an Ubuntu or Debian image for deploy (evo image list ubuntu), or create a plain VM from this image: evo vm create <name> --image <image>.",
			"Для deploy возьмите образ Ubuntu или Debian (evo image list ubuntu), а ВМ из этого образа создайте без deploy: evo vm create <имя> --image <образ>."))
}
