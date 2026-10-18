// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// InitOptions holds parameters for the init command.
type InitOptions struct {
	Path           string // config file to create
	NonInteractive bool   // pick the first zone and the preferred image without asking
	// Image is the boot image the person named (a name, a display name or
	// a UUID); "" — chosen from the Ubuntu and Debian images.
	Image string
}

// Init writes a project config with an availability zone and a boot image
// taken from the Cloud.ru API, not from a list baked into the binary: zone
// and image UUIDs differ between tenants and change over time. Without a
// terminal (or with NonInteractive) it takes the first zone of initZones
// (one with a subnet of the project, ru.AZ-2 first) and the newest plain
// Ubuntu (else Debian) image and prints what it chose.
func (e *Engine) Init(ctx context.Context, opts InitOptions) error {
	if _, err := os.Stat(opts.Path); err == nil {
		e.Printer.Warning(opts.Path + i18n.T(" already exists. Skipping.", " уже существует, файл не изменён."))
		return nil
	}
	// deploy reads this compose file; without it the first deploy fails
	// after the setup is done, so say it now. A warning: the file may come
	// later.
	if f := e.Config.ComposeFile; f != "" {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			e.Printer.Warning(i18n.Tf("%s not found: 'evo deploy' needs it (another file: compose_file in %s or 'evo deploy -f')",
				"%s не найден: он нужен 'evo deploy' (другой файл — compose_file в %s или 'evo deploy -f')", f, opts.Path))
		}
	}

	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	e.Cloud.SetToken(tok.Value)

	zones, err := e.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return fmt.Errorf(i18n.T("list zones: %w", "список зон: %w"), err)
	}
	if len(zones) == 0 {
		return clerrors.New(clerrors.CodeAPIError, i18n.T("the API returned no availability zones", "API не вернул ни одной зоны доступности"),
			i18n.T("Check the project in the Cloud.ru console.", "Проверьте проект в консоли Cloud.ru."))
	}
	var image *provision.Image
	imageLine := ""
	if opts.Image != "" {
		image, imageLine, err = e.initImage(ctx, opts.Image)
		if err != nil {
			return err
		}
	}
	var candidates []*provision.Image
	if image == nil {
		list, err := e.Cloud.ListImages(ctx)
		if err != nil {
			return fmt.Errorf(i18n.T("list images: %w", "список образов: %w"), err)
		}
		candidates = deployableImages(list)
		if len(candidates) == 0 {
			return clerrors.New(clerrors.CodeConfigInvalid,
				i18n.T("no Ubuntu or Debian image found in the image catalog", "в каталоге образов нет Ubuntu или Debian"),
				i18n.Tf("Pick an image with 'evo image list' and pass it: evo init --image <name>, or set image in %s by hand.",
					"Выберите образ ('evo image list') и передайте его: evo init --image <имя> — или впишите image в %s вручную.", opts.Path))
		}
	}

	interactive := !opts.NonInteractive && confirm.StdinIsTTY()

	if open := provision.OpenZones(zones); len(open) < len(zones) {
		for _, z := range zones {
			if z.Closed() {
				e.Printer.Warning(i18n.Tf("zone %s is closed for new resources (enabled=false in the zone list) and is not offered",
					"зона %s закрыта для новых ресурсов (в списке зон enabled=false) и не предлагается", z.Name))
			}
		}
		zones = open
	}
	zones = e.initZones(ctx, zones)
	zoneNames := make([]string, len(zones))
	for i, z := range zones {
		zoneNames[i] = z.Name
	}
	zi, err := e.choose(ctx, interactive, i18n.T("Availability zone", "Зона доступности"), zoneNames)
	if err != nil {
		return err
	}
	zone := zones[zi]

	if image == nil {
		imageNames := make([]string, len(candidates))
		for i, img := range candidates {
			imageNames[i] = img.Name
		}
		ii, err := e.choose(ctx, interactive, i18n.T("Boot image", "Загрузочный образ"), imageNames)
		if err != nil {
			return err
		}
		image = candidates[ii]
		imageLine = "image_id: " + yamlComment(image.ID) + "  # " + yamlComment(image.Name)
	}

	e.Printer.StepDone(i18n.Tf("Zone: %s (%s)", "Зона: %s (%s)", zone.Name, zone.ID))
	e.Printer.StepDone(i18n.Tf("Image: %s (%s)", "Образ: %s (%s)", image.Name, image.ID))

	projectLine := i18n.T("# project_id: <your-project-uuid>", "# project_id: <UUID вашего проекта>")
	if e.Config.ProjectID != "" {
		projectLine = "project_id: " + e.Config.ProjectID
	}
	content := fmt.Sprintf(i18n.T(initTemplateEN, initTemplateRU),
		yamlComment(zone.ID), yamlComment(zone.Name), imageLine, projectLine)

	if err := os.WriteFile(opts.Path, []byte(content), 0o644); err != nil {
		return fmt.Errorf(i18n.T("failed to create config: %w", "не удалось создать файл настроек: %w"), err)
	}

	e.Printer.Success(i18n.T("Created ", "Создан ") + opts.Path)
	if e.Config.ProjectID == "" {
		e.Printer.Hint(i18n.Tf("Set project_id in %s, then run 'evo deploy'.", "Впишите project_id в %s и выполните 'evo deploy'.", opts.Path))
	} else {
		e.Printer.Hint(i18n.T("Run 'evo deploy'.", "Дальше: 'evo deploy'."))
	}
	return nil
}

// preferredZone is the default zone of init: offered first and taken
// without a terminal. 15.10.2026 ru.AZ-1 failed three cloud-e2e runs in a row
// (a VM 10–20 min in creating, error_creating, no SSH after boot) while
// ru.AZ-2 passed every one; the API lists ru.AZ-1 first.
const preferredZone = "ru.AZ-2"

// PreferredZoneFirst moves preferredZone to the front, keeping the order of
// the rest; zones without it are returned as they are.
func PreferredZoneFirst(zones []*provision.AvailabilityZone) []*provision.AvailabilityZone {
	for i, z := range zones {
		if z.Name == preferredZone {
			out := make([]*provision.AvailabilityZone, 0, len(zones))
			out = append(out, z)
			out = append(out, zones[:i]...)
			return append(out, zones[i+1:]...)
		}
	}
	return zones
}

// initZones orders the zones for init: first a zone where the project has
// a subnet — deploy cannot plug a VM in elsewhere, and a new project has
// one only in ru.AZ-1 (docs, «Подсети»: Default_ru.AZ-1 is created
// automatically). Among them ru.AZ-2 (preferredZone) if it has one, else a
// zone with the project's main subnet (the API's default flag), else the
// first; then the other zones. It says why when preferredZone is not
// first, and warns when no zone has a subnet. Without a project or the
// subnet list the order is PreferredZoneFirst.
func (e *Engine) initZones(ctx context.Context, zones []*provision.AvailabilityZone) []*provision.AvailabilityZone {
	zones = PreferredZoneFirst(zones)
	if e.Config.ProjectID == "" {
		return zones
	}
	subnets, err := e.Cloud.ListSubnets(ctx, e.Config.ProjectID)
	if err != nil {
		return zones
	}
	rank := func(z *provision.AvailabilityZone) int {
		has, main := false, false
		for _, s := range subnets {
			if provision.SubnetInZone(s, z.ID, z.Name) {
				has, main = true, main || s.Default
			}
		}
		switch {
		case has && z.Name == preferredZone:
			return 0
		case has && main:
			return 1
		case has:
			return 2
		}
		return 3
	}
	sort.SliceStable(zones, func(i, j int) bool { return rank(zones[i]) < rank(zones[j]) })
	switch {
	case len(zones) == 0:
	case rank(zones[0]) == 3:
		e.Printer.Warning(i18n.T("the project has no subnet in any zone: deploy cannot plug a VM in. Create a subnet in the Cloud.ru console (Network → Subnets), then run 'evo init' again",
			"в проекте нет подсети ни в одной зоне: deploy не сможет подключить ВМ к сети. Создайте подсеть в консоли Cloud.ru (Сеть → Подсети) и повторите 'evo init'"))
	case zones[0].Name != preferredZone:
		for _, z := range zones {
			if z.Name == preferredZone {
				e.Printer.Hint(i18n.Tf("%s is not offered first: the project has no subnet there; %s has one",
					"%s не предлагается первой: в ней нет подсети проекта; подсеть есть в %s", preferredZone, zones[0].Name))
				break
			}
		}
	}
	return zones
}

// initTemplateEN and initTemplateRU are the generated project file; the
// arguments are the zone ID and name, the image line and the project_id
// line.
const initTemplateRU = `# Настройки evo
# Подробнее: https://github.com/shatim-ops/evo-cli

# Зона доступности (evo list-zones)
region: %s  # %s

# Загрузочный образ: image — имя из 'evo image list' или UUID, image_id — UUID.
# Docker ставится при первом deploy (Ubuntu, Debian).
%s

# Flavor ВМ: auto подбирает наименьший, которого хватит сервисам compose-файла
flavor: auto

# Размер загрузочного диска, ГБ (если меньше минимума образа, берётся минимум)
disk_size: 10

# Compose-файл для развёртывания
compose_file: docker-compose.yml

# Сколько секунд deploy ждёт, пока новая ВМ станет активной
timeout: 600

# Группа безопасности (имя или UUID), в которой deploy откроет порты compose.
# Без неё берётся группа зоны по умолчанию (SSH-access_<зона>), иначе первая по имени — с предупреждением.
# security_group: SSH-access_ru.AZ-2

# UUID проекта Cloud.ru (обязателен; console.cloud.ru или CLOUDRU_PROJECT_ID)
%s
`

const initTemplateEN = `# evo configuration
# See: https://github.com/shatim-ops/evo-cli

# Availability zone (evo list-zones)
region: %s  # %s

# Boot image: image is a name from 'evo image list' or a UUID, image_id a UUID.
# Docker is installed on first deploy (Ubuntu, Debian).
%s

# VM flavor: auto picks the smallest that fits your compose services
flavor: auto

# Boot disk size in GB (raised to the image minimum if smaller)
disk_size: 10

# Docker Compose file to deploy
compose_file: docker-compose.yml

# Seconds deploy waits for a new VM to become active
timeout: 600

# Security group (name or UUID) deploy opens the compose ports in. Without it:
# the zone's default group (SSH-access_<zone>), else the first by name, with a warning.
# security_group: SSH-access_ru.AZ-2

# Cloud.ru project UUID (required; console.cloud.ru or CLOUDRU_PROJECT_ID)
%s
`

// choose returns the index of the option picked by the user, or 0 when not
// interactive. An empty answer takes the first option.
func (e *Engine) choose(ctx context.Context, interactive bool, what string, options []string) (int, error) {
	if !interactive || len(options) == 1 {
		return 0, nil
	}
	w := e.Printer.ErrWriter()
	fmt.Fprintf(w, "%s:\n", what)
	for i, o := range options {
		fmt.Fprintf(w, "  %d) %s\n", i+1, o)
	}
	fmt.Fprint(w, i18n.T("Choose [1]: ", "Выберите [1]: "))
	answer, err := confirm.ReadLine(ctx)
	if err != nil {
		return 0, err
	}
	if answer == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(answer)
	if err != nil || n < 1 || n > len(options) {
		return 0, clerrors.New(clerrors.CodeUsage,
			i18n.Tf("invalid choice %q for %s", "недопустимый выбор %q: %s", answer, strings.ToLower(what)),
			i18n.Tf("Enter a number from 1 to %d, or re-run with --non-interactive.",
				"Введите число от 1 до %d или повторите с --non-interactive.", len(options)))
	}
	return n - 1, nil
}

// Plain distribution images ("Ubuntu-24.04", "ubuntu-22.04", "debian-12"):
// the Docker install script supports them. Marketplace builds, GPU and
// desktop variants carry suffixes and do not match.
var (
	ubuntuImageRe = regexp.MustCompile(`(?i)^ubuntu[-_ ]?(\d{2})\.(\d{2})$`)
	debianImageRe = regexp.MustCompile(`(?i)^debian[-_ ]?(\d{1,2})$`)
)

// deployableImages returns the images deploy can install Docker on, best
// first: Ubuntu by version descending, then Debian by version descending.
func deployableImages(images []*provision.Image) []*provision.Image {
	type ranked struct {
		img     *provision.Image
		family  int // 0 ubuntu, 1 debian
		version int
	}
	var out []ranked
	for _, img := range images {
		if !img.Usable() {
			continue
		}
		if m := ubuntuImageRe.FindStringSubmatch(img.Name); m != nil {
			major, _ := strconv.Atoi(m[1])
			minor, _ := strconv.Atoi(m[2])
			out = append(out, ranked{img, 0, major*100 + minor})
		} else if m := debianImageRe.FindStringSubmatch(img.Name); m != nil {
			v, _ := strconv.Atoi(m[1])
			out = append(out, ranked{img, 1, v})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].family != out[j].family {
			return out[i].family < out[j].family
		}
		return out[i].version > out[j].version
	})
	res := make([]*provision.Image, len(out))
	for i, r := range out {
		res[i] = r.img
	}
	return res
}

// yamlComment keeps a value from the cloud on its own line of the generated
// config: a zone or image name with a newline would otherwise add keys
// (compose_file, flavor, ...) to the user's project file.
func yamlComment(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// initImage resolves the image given to init (images.Resolve) and makes its
// line of the project file: a name as the catalogue spells it (image:), a
// UUID as given (image_id:). A name that would not resolve back to the same
// image at deploy (a copy with the same name elsewhere in the catalogue) is
// written as its UUID. An image deploy cannot put Docker on, or one the
// project cannot use, is refused here, not at the first deploy.
func (e *Engine) initImage(ctx context.Context, ref string) (*provision.Image, string, error) {
	cat := images.Catalog{Cloud: e.Cloud, ProjectID: e.Config.ProjectID}
	img, err := cat.Resolve(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	if img.NotInProject {
		return nil, "", images.NotInProjectError(img.Name)
	}
	if family := images.NoDockerFamily(img.Name, img.DisplayName, img.OS); family != "" {
		return nil, "", images.NoDockerError(clerrors.CodeUsage, img.Name, family)
	}
	if !img.Usable() && img.Type != provision.ImageTypePrivate {
		e.Printer.Warning(i18n.Tf("image %s is not ready in any zone yet: deploy may fail on it", "образ %s ещё не готов ни в одной зоне: deploy на нём может не пройти", img.Name))
	}
	idLine := "image_id: " + yamlComment(img.ID) + "  # " + yamlComment(img.Name)
	if images.IsUUID(strings.TrimSpace(ref)) {
		return img, idLine, nil
	}
	if back, err := cat.Resolve(ctx, img.Name); err != nil || back.ID != img.ID {
		return img, idLine, nil
	}
	return img, "image: " + yamlName(img.Name) + "  # " + yamlComment(img.ID), nil
}

// yamlName is an image name as a YAML scalar: quoted when YAML would read
// it otherwise (a colon, a leading symbol, "yes").
func yamlName(s string) string {
	s = yamlComment(s)
	b, err := yaml.Marshal(s)
	if err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimSuffix(string(b), "\n")
}
