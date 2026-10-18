// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package marketplace

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Витрина на поддельных каталогах. Все UUID выдуманы.

const (
	testProject = "00000000-0000-0000-0000-0000000000f1"
	testZone    = "00000000-0000-0000-0000-00000000a001"
)

func id(n int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }

// imageCloud ищет как сервер: name= и display_name= — подстрока без учёта
// регистра. С project_id= — каталог, каким его видит проект: без hidden;
// projErr — ошибка такого вызова, err — любого.
type imageCloud struct {
	shared  []*provision.Image
	hidden  map[string]bool
	projErr error
	err     error
	ownRead bool
}

func (c *imageCloud) ListImages(_ context.Context, filter ...provision.ImageFilter) ([]*provision.Image, error) {
	if c.err != nil {
		return nil, c.err
	}
	var f provision.ImageFilter
	if len(filter) > 0 {
		f = filter[0]
	}
	if f.ProjectID != "" && c.projErr != nil {
		return nil, c.projErr
	}
	var out []*provision.Image
	for _, img := range c.shared {
		if f.ProjectID != "" && c.hidden[img.ID] {
			continue
		}
		if f.Name != "" && !strings.Contains(strings.ToLower(img.Name), strings.ToLower(f.Name)) ||
			f.DisplayName != "" && !strings.Contains(strings.ToLower(img.DisplayName), strings.ToLower(f.DisplayName)) {
			continue
		}
		cp := *img
		out = append(out, &cp)
	}
	return out, nil
}

func (c *imageCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	c.ownRead = true
	return []*provision.Image{{ID: id(99), Name: "my-golden-image"}}, nil
}

func (c *imageCloud) GetImage(context.Context, string) (*provision.ImageDetail, error) {
	return nil, stderrors.New("not used")
}

type addonCatalog struct {
	list []mk8s.Addon
	err  error
}

func (a addonCatalog) ListAddons(context.Context) ([]mk8s.Addon, error) { return a.list, a.err }

func zones(state string) []provision.ImageAvailZone {
	return []provision.ImageAvailZone{{AvailabilityZoneID: testZone, AvailabilityZoneName: "ru.AZ-1", State: state}}
}

func testSources() (Sources, *imageCloud) {
	cloud := &imageCloud{
		shared: []*provision.Image{
			{ID: id(1), Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Type: "public", AvailZones: zones("created")},
			{ID: id(2), Name: "AstraLinuxSmolensk1.8.3KES", DisplayName: "Astra Linux Смоленск 1.8.3 KES", Type: "marketplace", AvailZones: zones("created")},
			{ID: id(3), Name: "nginx-ubuntu-22.04", DisplayName: "Nginx на Ubuntu 22.04", Type: "marketplace", AvailZones: zones("created")},
			{ID: id(4), Name: "redos-8", DisplayName: "РЕД ОС 8", Type: "marketplace", AvailZones: zones("created")},
			{ID: id(5), Name: "OpensearchDashboards", DisplayName: "OpenSearch Dashboards", Type: "marketplace", AvailZones: zones("created")},
			{ID: id(6), Name: "broken-gitlab", DisplayName: "GitLab", Type: "marketplace", AvailZones: zones("error")},
			{ID: id(7), Name: "WindowsServer2022", DisplayName: "Windows Server 2022", Type: "marketplace", AvailZones: zones("created")},
		},
		hidden: map[string]bool{id(2): true},
	}
	addons := addonCatalog{list: []mk8s.Addon{
		{Name: "traefik", DisplayName: "Traefik", Description: "Ingress controller", IsBillable: false},
		{Name: "cert-manager", DisplayName: "Cert Manager", Description: "Выпуск TLS-сертификатов", IsBillable: false},
		{Name: "gpu-operator", DisplayName: "GPU Operator", IsBillable: true},
	}}
	return Sources{Images: &images.Catalog{Cloud: cloud, ProjectID: testProject}, Addons: addons}, cloud
}

func itemNames(res *Result) string {
	var out []string
	for _, it := range res.Items {
		out = append(out, it.Kind+":"+it.Name)
	}
	return strings.Join(out, ",")
}

// Оба каталога, сортировка по виду и отображаемому имени, скрытые образы
// по причинам, образы проекта (private) не читаются.
func TestList_MergeSortHide(t *testing.T) {
	src, cloud := testSources()
	res, err := List(context.Background(), src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := "vm-os:Ubuntu-24.04,vm-os:WindowsServer2022,vm-os:redos-8," +
		"vm-app:nginx-ubuntu-22.04,vm-app:OpensearchDashboards," +
		"k8s-addon:cert-manager,k8s-addon:gpu-operator,k8s-addon:traefik"
	if got := itemNames(res); got != want {
		t.Errorf("items:\n got %s\nwant %s", got, want)
	}
	if res.Total != 10 || res.HiddenNotInProject != 1 || res.HiddenNotReady != 1 || res.Hidden() != 2 {
		t.Errorf("total %d hidden %d/%d", res.Total, res.HiddenNotInProject, res.HiddenNotReady)
	}
	if res.Counts[KindOS] != 3 || res.Counts[KindApp] != 2 || res.Counts[KindAddon] != 3 {
		t.Errorf("counts: %v", res.Counts)
	}
	if !res.InProjectChecked || len(res.Warnings) != 0 {
		t.Errorf("checked %v warnings %v", res.InProjectChecked, res.Warnings)
	}
	if cloud.ownRead {
		t.Error("the project's own images were read")
	}

	all, err := List(context.Background(), src, Options{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 10 || all.Hidden() != 0 {
		t.Errorf("--all: %s", itemNames(all))
	}
	for _, it := range all.Items {
		if it.Name == "AstraLinuxSmolensk1.8.3KES" && (it.Kind != KindOS || !it.Image.NotInProject) {
			t.Errorf("astra: %+v", it)
		}
	}
}

func TestList_QueryAndKind(t *testing.T) {
	src, _ := testSources()
	ctx := context.Background()
	cases := []struct {
		opt  Options
		want string
	}{
		// Подстрока имени или отображаемого имени, без учёта регистра;
		// у плагинов — и описания.
		{Options{Query: "UBUNTU"}, "vm-os:Ubuntu-24.04,vm-app:nginx-ubuntu-22.04"},
		{Options{Query: "tls"}, "k8s-addon:cert-manager"},
		{Options{Query: "ред ос"}, "vm-os:redos-8"},
		{Options{Kind: KindApp}, "vm-app:nginx-ubuntu-22.04,vm-app:OpensearchDashboards"},
		{Options{Kind: KindAddon, Query: "gpu"}, "k8s-addon:gpu-operator"},
		{Options{Kind: KindOS, All: true, Query: "astra"}, "vm-os:AstraLinuxSmolensk1.8.3KES"},
	}
	for _, c := range cases {
		res, err := List(ctx, src, c.opt)
		if err != nil {
			t.Fatalf("%+v: %v", c.opt, err)
		}
		if got := itemNames(res); got != c.want {
			t.Errorf("%+v:\n got %s\nwant %s", c.opt, got, c.want)
		}
	}
	// Вид фильтрует и итог: скрытый vm-app не попадает в счёт vm-os.
	res, _ := List(ctx, src, Options{Kind: KindOS})
	if res.Total != 4 || res.HiddenNotInProject != 1 || res.HiddenNotReady != 0 {
		t.Errorf("vm-os: total %d hidden %d/%d", res.Total, res.HiddenNotInProject, res.HiddenNotReady)
	}

	if _, err := List(ctx, src, Options{Kind: "vm"}); err == nil || !strings.HasPrefix(err.Error(), clierrors.CodeUsage) {
		t.Errorf("bad kind: %v", err)
	}
}

// Один каталог недоступен — второй показан, с предупреждением; оба — ошибка.
// Вид, которому нужен только упавший каталог, — тоже ошибка.
func TestList_PartialFailure(t *testing.T) {
	ctx := context.Background()
	src, _ := testSources()
	down := clierrors.New(clierrors.CodeAPIUnreachable, "mk8s: 503 no healthy upstream", "")
	src.Addons = addonCatalog{err: down}
	res, err := List(ctx, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Counts[KindAddon] != 0 || res.Counts[KindOS] != 3 || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "503") {
		t.Errorf("counts %v warnings %v", res.Counts, res.Warnings)
	}
	if _, err := List(ctx, src, Options{Kind: KindAddon}); !stderrors.Is(err, down) {
		t.Errorf("only addons, down: %v", err)
	}

	src, cloud := testSources()
	cloud.err = stderrors.New("compute: 500")
	res, err = List(ctx, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if itemNames(res) != "k8s-addon:cert-manager,k8s-addon:gpu-operator,k8s-addon:traefik" || len(res.Warnings) != 1 || res.InProjectChecked {
		t.Errorf("images down: %s %v", itemNames(res), res.Warnings)
	}

	src.Addons = addonCatalog{err: down}
	_, err = List(ctx, src, Options{})
	var ce *clierrors.CLIError
	if err == nil || !strings.Contains(err.Error(), "compute: 500") || !strings.Contains(err.Error(), "503") || !stderrors.As(err, &ce) {
		t.Errorf("both down: %v", err)
	}
}

// Каталог, каким его видит проект, не прочитан: ничего не помечено и не
// скрыто по этой причине, доступность не известна, предупреждение есть.
func TestList_ProjectViewFails(t *testing.T) {
	src, cloud := testSources()
	cloud.projErr = stderrors.New("403")
	res, err := List(context.Background(), src, Options{Kind: KindOS})
	if err != nil {
		t.Fatal(err)
	}
	if res.InProjectChecked || res.HiddenNotInProject != 0 || len(res.Warnings) != 1 {
		t.Errorf("checked %v hidden %d warnings %v", res.InProjectChecked, res.HiddenNotInProject, res.Warnings)
	}
	if !strings.Contains(itemNames(res), "AstraLinuxSmolensk1.8.3KES") {
		t.Errorf("items: %s", itemNames(res))
	}
	// Без проекта — тоже не проверено, без предупреждения.
	src.Images.ProjectID = ""
	cloud.projErr = nil
	res, _ = List(context.Background(), src, Options{})
	if res.InProjectChecked || len(res.Warnings) != 0 {
		t.Errorf("no project: checked %v warnings %v", res.InProjectChecked, res.Warnings)
	}
}

// Два образа с одним именем: команда называет UUID, имя было бы
// неоднозначным для vm create --image.
func TestList_DuplicateNameUsesID(t *testing.T) {
	src, cloud := testSources()
	cloud.shared = append(cloud.shared, &provision.Image{ID: id(8), Name: "ubuntu-24.04", DisplayName: "Ubuntu 24.04 (copy)", Type: "public", AvailZones: zones("created")})
	res, err := List(context.Background(), src, Options{Query: "24.04"})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range res.Items {
		if it.ImageRef != it.Image.ID {
			t.Errorf("%s: ref %q, want the UUID", it.Name, it.ImageRef)
		}
	}
	res, _ = List(context.Background(), src, Options{Query: "opensearch"})
	if res.Items[0].ImageRef != "OpensearchDashboards" {
		t.Errorf("unique name: ref %q", res.Items[0].ImageRef)
	}
}

func TestImageKind(t *testing.T) {
	cases := []struct {
		name, display, typ, want string
	}{
		{"Ubuntu-24.04", "Ubuntu 24.04", "public", KindOS},
		{"some-tool", "Some tool", "public", KindOS}, // public — всегда ОС
		{"AstraLinuxSmolensk1.8.3KES", "Astra Linux Смоленск 1.8.3 KES", "marketplace", KindOS},
		{"alse-base-1-7-7", "Astra Linux SE «Базовый» 1.7.7", "marketplace", KindOS}, // по отображаемому имени
		{"nginx-ubuntu-22.04", "Nginx (Ubuntu 22.04)", "marketplace", KindApp},
		{"redos-8", "", "marketplace", KindOS},
		{"RED-OS-7.3", "", "marketplace", KindOS},
		{"x1", "РЕД ОС 8 сервер", "marketplace", KindOS},
		{"WindowsServer2022", "", "marketplace", KindOS},
		{"alt-server-10", "", "marketplace", KindOS},
		{"AltServer10", "", "marketplace", KindOS},
		{"x2", "Альт Рабочая станция 10", "marketplace", KindOS},
		{"altair-cad", "Altair CAD", "marketplace", KindApp},
		{"SberLinux-9", "", "marketplace", KindOS},
		{"rosa-chrome-12", "", "marketplace", KindOS},
		{"x3", "РОСА Хром 12", "marketplace", KindOS},
		{"rosatom-app", "", "marketplace", KindApp},
		{"AlmaLinux-9", "", "marketplace", KindOS},
		{"almanac", "Almanac", "marketplace", KindApp},
		{"openSUSE-Leap-15.5", "", "marketplace", KindOS},
		{"  Debian-12", "", "marketplace", KindOS}, // пробелы в начале не мешают
		{"rocky-9", "", "", KindOS},                // тип не указан — как marketplace
		{"OpensearchDashboards", "OpenSearch Dashboards", "marketplace", KindApp},
		{"gitlab-ce", "GitLab CE на Debian 12", "marketplace", KindApp},
	}
	for _, c := range cases {
		got := ImageKind(&provision.Image{Name: c.name, DisplayName: c.display, Type: c.typ})
		if got != c.want {
			t.Errorf("%q / %q (%s): %s, want %s", c.name, c.display, c.typ, got, c.want)
		}
	}
}
