// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Превью платных команд деревьев (ревью r5 U2): проект с источником, зона
// по имени, образ, flavor с размером, диск и цена с пометкой «оценка» — как
// у deploy, без английского текста инструмента и без MCP-жаргона.

// All IDs below are fake.
const (
	pvProject = "00000000-0000-0000-0000-0000000000a1"
	pvZone    = "00000000-0000-0000-0000-0000000000b2"
	pvImage   = "00000000-0000-0000-0000-0000000000c3"
	pvFlavor  = "00000000-0000-0000-0000-0000000000d4"
)

type previewToken struct{}

func (previewToken) Token(context.Context) (auth.Token, error) {
	return auth.Token{Value: "fake-token", ExpiresAt: clock.Now().Add(time.Hour)}, nil
}

// previewCloud answers the reads of the previews; any other call (a create)
// hits the nil interface and fails the test.
type previewCloud struct {
	provision.CloudAPI
}

func (previewCloud) SetToken(string) {}
func (previewCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{
		{ID: "00000000-0000-0000-0000-0000000000f0", Name: "free-tier1", VCPUs: 1, RAMGB: 1, FreeTier: true, Share: "1:1", GPU: new(int), Type: &provision.FlavorType{},
			Zones: []provision.FlavorZone{{ID: pvZone, Name: "ru.AZ-1", Enabled: true}}},
		{ID: "00000000-0000-0000-0000-0000000000e5", Name: "lowcost10-4-8", VCPUs: 4, RAMGB: 8, Share: "1:10", GPU: new(int), Type: &provision.FlavorType{},
			Zones: []provision.FlavorZone{{ID: pvZone, Name: "ru.AZ-1", Enabled: true}}},
		{ID: pvFlavor, Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4, Share: "1:10", GPU: new(int), Type: &provision.FlavorType{},
			Zones: []provision.FlavorZone{{ID: pvZone, Name: "ru.AZ-1", Enabled: true}}},
	}, nil
}
func (previewCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: pvZone, Name: "ru.AZ-1"}}, nil
}
func (previewCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: id, Name: "Ubuntu 22.04", MinDisk: 20}, nil
}
func (previewCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return []*provision.Image{{ID: pvImage, Name: "Ubuntu 22.04"}}, nil
}
func (previewCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return []*provision.DiskType{{ID: "00000000-0000-0000-0000-0000000000e6", Name: "SSD"}}, nil
}

func (previewCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{{ID: "00000000-0000-0000-0000-0000000000d1", Name: "Default_ru.AZ-1", AvailabilityZoneID: pvZone, AvailabilityZoneName: "ru.AZ-1", Default: true}}, nil
}
func (previewCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "00000000-0000-0000-0000-0000000000c1", Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: pvZone, AvailabilityZoneName: "ru.AZ-1", Default: true,
		Rules: []provision.SGRule{
			{Direction: "ingress", IPProtocol: "tcp", PortRange: "22:22", RemoteIPPrefix: "0.0.0.0/0"},
			{Direction: "egress", IPProtocol: "any", RemoteIPPrefix: "0.0.0.0/0"},
		}}}, nil
}

// closedNetCloud: the zone's subnet has no DNS servers and the group lets
// only TCP 80 in.
type closedNetCloud struct{ previewCloud }

func (closedNetCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	none := []any{}
	return []*provision.Subnet{{ID: "00000000-0000-0000-0000-0000000000d1", Name: "Default_ru.AZ-1", AvailabilityZoneID: pvZone, DNSServers: &none}}, nil
}
func (closedNetCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "00000000-0000-0000-0000-0000000000c1", Name: "web_ru.AZ-1", AvailabilityZoneID: pvZone,
		Rules: []provision.SGRule{{Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80", RemoteIPPrefix: "0.0.0.0/0"}}}}, nil
}

// Превью evo vm create предупреждает о сети до вопроса: подсеть без DNS,
// группа без входящего TCP 22 и без исходящего системного трафика.
func TestVMCreatePreview_NetworkWarnings(t *testing.T) {
	russian(t)
	withCloud(t, closedNetCloud{})
	_, errOut, code := execCLI(t, "vm", "create", "web")
	if code != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit %d, want 3:\n%s", code, errOut)
	}
	assertHas(t, errOut, []string{
		"внимание:   в подсети Default_ru.AZ-1 не указаны DNS-серверы",
		"внимание:   в группе безопасности web_ru.AZ-1 нет входящего правила для TCP 22",
		"внимание:   группа безопасности web_ru.AZ-1 не разрешает исходящий системный трафик",
	})
}

// withPreviewCloud configures project, zone and image in the environment
// and gives the engines of the tools a fake cloud and a token.
func withPreviewCloud(t *testing.T) {
	t.Helper()
	withCloud(t, previewCloud{})
}

// withCloud is withPreviewCloud with the given fake cloud.
func withCloud(t *testing.T, cloud provision.CloudAPI) {
	t.Helper()
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	t.Setenv("CLOUDRU_REGION", pvZone)
	t.Setenv("CLOUDRU_IMAGE_ID", pvImage)
	t.Setenv("CLOUDRU_FLAVOR", "")
	t.Setenv("CLOUDRU_DISK_SIZE", "")
	old := toolEngine
	stoppedClock(t)
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth, e.Cloud = previewToken{}, cloud
		return e, nil
	}
	t.Cleanup(func() { toolEngine = old })
}

// jargon is what the preview used to show: the tool's English text for an
// agent and its placeholders.
var jargon = []string{"confirmed=true", "deploy_plan", "default (env/config or auto)", "will_create", "call provision", "вызвать provision"}

func assertNoJargon(t *testing.T, out string) {
	t.Helper()
	for _, j := range jargon {
		if strings.Contains(out, j) {
			t.Errorf("preview still shows %q:\n%s", j, out)
		}
	}
}

func assertHas(t *testing.T, out string, want []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("preview lacks %q:\n%s", w, out)
		}
	}
}

func TestVMCreatePreview_Russian(t *testing.T) {
	russian(t)
	withPreviewCloud(t)
	_, errOut, code := execCLI(t, "vm", "create", "web")
	if code != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit %d, want 3:\n%s", code, errOut)
	}
	t.Logf("\n%s", errOut)
	assertHas(t, errOut, []string{
		"План: создать ВМ web (lowcost10-2-4) в зоне ru.AZ-1\n",
		"  Будет создано: ВМ web, загрузочный диск 20 ГБ, floating IP\n",
		"  проект:     " + pvProject + " (из CLOUDRU_PROJECT_ID)\n",
		"  flavor:     lowcost10-2-4 (2 vCPU, доля vCPU 1:10, 4 ГБ RAM)\n",
		"  зона:       ru.AZ-1 (" + pvZone + ")\n",
		"  образ:      Ubuntu 22.04 (" + pvImage + ")\n",
		"  диск:       загрузочный, 20 ГБ (цена по тарифу SSD)\n", // raised to the image's min_disk
		"  внешний IP: будет создан и привязан floating IP\n",
		"  цена:       ~2184.00 RUB в месяц (вычисления 2044.00 + диск 140.00), оценка по тарифной карте evo, а не счёт\n",
		"  цена API:   не получена (price-calculation: not in this fake), показана оценка\n",
		"provision не выполняется: нужно подтверждение, а stdin — не терминал",
	})
	assertNoJargon(t, errOut)
	if strings.Contains(errOut, "project:") || strings.Contains(errOut, "billable") {
		t.Errorf("English left in the preview:\n%s", errOut)
	}
}

func TestVMCreatePreview_English(t *testing.T) {
	withPreviewCloud(t)
	out, _, code := execCLI(t, "vm", "create", "web", "--no-public-ip", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	assertHas(t, out, []string{
		"Dry run: would create VM web (lowcost10-2-4) in ru.AZ-1:\n",
		"  Will create: VM web, a 20 GB boot disk\n",
		"  project:   " + pvProject + " (from CLOUDRU_PROJECT_ID)\n",
		"  flavor:    lowcost10-2-4 (2 vCPU, vCPU share 1:10, 4 GB RAM)\n",
		"  zone:      ru.AZ-1 (" + pvZone + ")\n",
		"  image:     Ubuntu 22.04 (" + pvImage + ")\n",
		"  disk:      20 GB boot disk (priced at the SSD rate)\n",
		"  public IP: none (private VM, no floating IP)\n",
		"  cost:      ~2184.00 RUB/month (compute 2044.00 + disk 140.00), evo rate-card estimate, not a bill\n",
		"  API price: none (price-calculation: not in this fake), the estimate is shown\n",
	})
	assertNoJargon(t, out)
}

// Without credentials the preview still names what the configuration says
// and leaves out what only the cloud can tell (no placeholders); the exit
// code stays 3. (Without a zone or an image there is no preview:
// TestVMCreate_NoZoneRefusedBeforeConsent.)
func TestVMCreatePreview_NoCloudOmitsUnknown(t *testing.T) {
	russian(t)
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	t.Setenv("CLOUDRU_REGION", pvZone)
	t.Setenv("CLOUDRU_IMAGE_ID", pvImage)
	_, errOut, code := execCLI(t, "vm", "create", "web")
	if code != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit %d, want 3:\n%s", code, errOut)
	}
	assertHas(t, errOut, []string{
		"План: создать ВМ web в зоне " + pvZone + "\n",
		"  проект:     " + pvProject + " (из CLOUDRU_PROJECT_ID)\n",
		"  зона:       " + pvZone + "\n",
		"  образ:      " + pvImage + "\n",
		"  цена:       не оценена: размер flavor неизвестен",
	})
	assertNoJargon(t, errOut)
	if strings.Contains(errOut, "flavor:") || strings.Contains(errOut, "не задано") {
		t.Errorf("unknown values shown:\n%s", errOut)
	}
}

// --json keeps the tool's answer under details (will_create, message) and
// adds the completed preview with the estimate.
func TestVMCreatePreview_JSONIsAdditive(t *testing.T) {
	withPreviewCloud(t)
	out, _, code := execCLI(t, "vm", "create", "web", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var env struct {
		Status  string `json:"status"`
		Preview struct {
			Action  string `json:"action"`
			Details struct {
				Status     string               `json:"status"`
				Message    string               `json:"message"`
				WillCreate map[string]any       `json:"will_create"`
				Preview    engine.CreatePreview `json:"preview"`
			} `json:"details"`
		} `json:"preview"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	d := env.Preview.Details
	if env.Status != "dry_run" || env.Preview.Action != "provision" || d.Status != "confirmation_required" ||
		d.Message == "" || d.WillCreate["name"] != "web" {
		t.Fatalf("existing keys changed: %s", out)
	}
	p := d.Preview
	if p.VM == nil || p.VM.Flavor.VCPUs != 2 || p.VM.Zone.Name != "ru.AZ-1" || p.Cost == nil || p.Cost.TotalMonthlyRub != 2184 ||
		p.ProjectID != pvProject || p.ProjectFrom != "env" {
		t.Fatalf("preview not completed: %s", out)
	}
}

func TestDiskCreatePreview_Russian(t *testing.T) {
	russian(t)
	withCloud(t, noDisksCloud{previewCloud{}})
	_, errOut, code := execCLI(t, "disk", "create", "data", "--size-gb", "20")
	if code != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit %d, want 3:\n%s", code, errOut)
	}
	assertHas(t, errOut, []string{
		"План: создать диск data (20 ГБ) в зоне ru.AZ-1\n",
		"  Будет создано: диск data\n",
		"  проект:     " + pvProject + " (из CLOUDRU_PROJECT_ID)\n",
		"  зона:       ru.AZ-1 (" + pvZone + ")\n",
		"  диск:       20 ГБ, тип SSD\n",
		"  цена:       ~140.00 RUB в месяц (диск), оценка по тарифной карте evo, а не счёт\n",
	})
	assertNoJargon(t, errOut)
	if strings.Contains(errOut, "disk_create makes") || strings.Contains(errOut, "zone_id") {
		t.Errorf("the tool's text left in the preview:\n%s", errOut)
	}
}

// noDisksCloud is a project without disks: a new disk's name is free, so
// the preview is reached (a taken name is refused before it, D49).
type noDisksCloud struct{ previewCloud }

func (noDisksCloud) ListDisks(context.Context, string) ([]map[string]any, error) { return nil, nil }

// A billable command without a rate card: header, project, zone and the
// key parameters, and that the cluster is paid, with its tariff — not a
// line that reads as «free» (dogfood 1.2.1).
func TestK8sCreatePreview_NoPrice(t *testing.T) {
	russian(t)
	withPreviewCloud(t)
	// The project has no cluster of the name, so the preview is reached (the
	// taken name, D21, is TestCreate_TakenNameRefusedNoSecondClusterOrBalancer);
	// the catalogue resolves the plan the preview names (D29).
	installContractTransport(t, 200, `{"clusters":[],`+k8sProductConfiguration[1:])
	_, errOut, code := execCLI(t, "k8s", "create", "--name", "k-1", "--zone", "ru.AZ-1", "--master-count", "3", "--kube-api-internet=false",
		"--cluster-sa-id", "00000000-0000-4000-8000-0000000000aa")
	if code != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit %d, want 3:\n%s", code, errOut)
	}
	assertHas(t, errOut, []string{
		"План: создать кластер Kubernetes k-1 в зоне ru.AZ-1\n",
		"  Будет создано: кластер Managed Kubernetes k-1\n",
		"  проект:     " + pvProject + " (из CLOUDRU_PROJECT_ID)\n",
		"  зона:       ru.AZ-1\n",
		"  flavor:     gen-2-4\n",
		"  vCPU:       2\n",
		"  RAM:        4 ГБ\n",
		"  версия:     v1.31.4\n",
		"  мастеров:   3\n",
		"  подсеть:    Default_ru.AZ-1 (00000000-0000-0000-0000-0000000000d1)\n",
		"  API извне:  нет\n",
		"  цена:       evo её не рассчитывает, ресурс платный: тариф https://cloud.ru/documents/tariffs/evolution/managed-kubernetes\n",
	})
	assertNoJargon(t, errOut)
	if strings.Contains(errOut, "k8s_plan") || strings.Contains(errOut, "billable") {
		t.Errorf("the tool's text left in the preview:\n%s", errOut)
	}
}

// noCloud is a cloud evo must not touch: every method, SetToken included,
// hits the nil interface.
type noCloud struct{ provision.CloudAPI }

// evo deploy apply asks with its saved plan (break 1.2, B12-8): the VM, the
// project and the monthly price deploy_plan quoted, read from the plan file
// alone — no token is set and no request is made before consent.
func TestDeployApplyPreview_ShowsThePlan(t *testing.T) {
	russian(t)
	dir := t.TempDir()
	plan := &deployplan.Plan{PlanID: "plan-0123456789ab", ProjectID: pvProject, ExpiresAt: time.Now().Add(time.Hour),
		VM: deployplan.VMSpec{Name: "web",
			Flavor: deployplan.FlavorSpec{ID: pvFlavor, Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4, Share: "1:10"},
			Image:  deployplan.RefSpec{ID: pvImage, Name: "Ubuntu 22.04"},
			Zone:   deployplan.RefSpec{ID: pvZone, Name: "ru.AZ-1"},
			Disk:   deployplan.DiskSpec{SizeGB: 20, Type: "SSD"}},
		Network: deployplan.NetworkSpec{FloatingIP: true},
		Ports:   []int{80, 443},
		Cost:    pricing.Estimate{Currency: "RUB", ComputeMonthlyRub: 1000, DiskMonthlyRub: 234.5, TotalMonthlyRub: 1234.5, Source: "estimate: rate-card"}}
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	old := toolEngine
	stoppedClock(t)
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.StateDir, e.Auth, e.Cloud = dir, previewToken{}, noCloud{}
		return e, nil
	}
	t.Cleanup(func() { toolEngine = old })
	_, errOut, code := execCLI(t, "deploy", "apply", plan.PlanID)
	if code != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit %d, want 3:\n%s", code, errOut)
	}
	t.Logf("\n%s", errOut)
	assertHas(t, errOut, []string{
		"План: создать ВМ web (lowcost10-2-4) в зоне ru.AZ-1\n",
		"  Будет создано: ВМ web, загрузочный диск 20 ГБ, floating IP\n",
		"  проект:     " + pvProject + " (из плана)\n",
		"  flavor:     lowcost10-2-4 (2 vCPU, доля vCPU 1:10, 4 ГБ RAM)\n",
		"  образ:      Ubuntu 22.04 (" + pvImage + ")\n",
		"  порты:      80,443 открываются для 0.0.0.0/0",
		"  цена:       ~1234.50 RUB в месяц (вычисления 1000.00 + диск 234.50), оценка по тарифной карте evo, а не счёт\n",
	})
	assertNoJargon(t, errOut)
}

// 'evo tools call' keeps the tool's answer as is (English, as in MCP).
func TestToolsCall_PreviewStaysRaw(t *testing.T) {
	i18n.SetLang(i18n.EN)
	withPreviewCloud(t)
	_, errOut, code := execCLI(t, "tools", "call", "provision", "--arg", "name=web")
	if code != clerrors.ExitConfirmationRequired || !strings.Contains(errOut, "provision creates a billable VM") {
		t.Errorf("exit %d:\n%s", code, errOut)
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (previewCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}
