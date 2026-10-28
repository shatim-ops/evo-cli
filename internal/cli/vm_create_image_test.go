// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// evo vm create --image <имя|UUID>, --meta и --port (шаг C) на поддельном
// каталоге newImageCloud. Все ID выдуманы.

const (
	vcSubnet = "00000000-0000-0000-0000-00000000b001"
	vcGroup  = "00000000-0000-0000-0000-00000000b002"
	vcVM     = "00000000-0000-0000-0000-00000000b003"
)

// vmCreateCloud — каталог плюс то, что нужно provision. Любая мутация
// записывается: тесты отказов проверяют, что их не было. fip — адрес,
// который резервируется до создания ВМ (nil — резервирование отказывает).
type vmCreateCloud struct {
	imageCloud
	mu        sync.Mutex
	creates   []provision.CreateVMRequest
	fips      int
	rules     []provision.SGRule
	mutations []string
	fip       *provision.FloatingIP
}

func (c *vmCreateCloud) note(m string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mutations = append(c.mutations, m)
}

func (*vmCreateCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{{ID: pvFlavor, Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4, Share: "1:10", GPU: new(int), Type: &provision.FlavorType{}}}, nil
}
func (*vmCreateCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{{ID: vcSubnet, Name: "subnet_ru.AZ-1"}}, nil
}
func (*vmCreateCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: vcGroup, Name: "Default_ru.AZ-1"}}, nil
}
func (*vmCreateCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return nil, nil
}
func (c *vmCreateCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.note("CreateFloatingIP")
	c.mu.Lock()
	c.fips++
	c.mu.Unlock()
	if c.fip != nil {
		fip := *c.fip
		return &fip, nil
	}
	return nil, clerrors.New(clerrors.CodeAPIError, "not in this test", "")
}
func (*vmCreateCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}
func (c *vmCreateCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	fip := *c.fip
	fip.State = "in_use"
	return &fip, nil
}
func (c *vmCreateCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.note("CreateVM")
	c.mu.Lock()
	c.creates = append(c.creates, req)
	c.mu.Unlock()
	return &provision.VM{ID: vcVM, Status: "creating"}, nil
}
func (*vmCreateCloud) GetVM(_ context.Context, id string) (*provision.VM, error) {
	return &provision.VM{ID: id, Status: "running"}, nil
}
func (*vmCreateCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return []*provision.SGRule{{ID: "rule-old", Direction: "ingress", IPProtocol: "tcp", PortRange: "22:22"}}, nil
}
func (c *vmCreateCloud) AddSecurityGroupRule(_ context.Context, _, _ string, rule provision.SGRule) (*provision.SGRule, error) {
	c.note("AddSecurityGroupRule")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules = append(c.rules, rule)
	rule.ID = "rule-" + rule.IPProtocol + "-" + rule.PortRange
	return &rule, nil
}

func withVMCreateCloud(t *testing.T) *vmCreateCloud {
	t.Helper()
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	t.Setenv("CLOUDRU_REGION", imgZone1)
	t.Setenv("CLOUDRU_IMAGE_ID", "")
	t.Setenv("CLOUDRU_FLAVOR", "")
	cloud := &vmCreateCloud{imageCloud: newImageCloud()}
	old := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth, e.Cloud = previewToken{}, cloud
		e.Timings.VMPollInterval = time.Millisecond
		return e, nil
	}
	t.Cleanup(func() { toolEngine = old })
	return cloud
}

func noMutations(t *testing.T, c *vmCreateCloud) {
	t.Helper()
	if len(c.mutations) > 0 {
		t.Errorf("отказ после изменений в облаке: %v", c.mutations)
	}
}

// Образ по имени без учёта регистра: в запрос создания идёт имя, как в
// каталоге. Astra Linux — обычная ВМ, vm create её не отклоняет. --meta
// побеждает, --port открывает порты в группе ВМ.
func TestVMCreate_ImageByNameMetaAndPorts(t *testing.T) {
	cloud := withVMCreateCloud(t)
	out, errOut, code := execCLI(t, "--json", "--yes", "vm", "create", "web", "--image", "opensearchdashboards",
		"--meta", "opensearch_host=db.internal", "--meta", "hostname=web-1", "--port", "80", "--port", "8080/tcp,53/udp", "--no-public-ip")
	if code != 0 {
		t.Fatalf("exit %d:\n%s\n%s", code, out, errOut)
	}
	if len(cloud.creates) != 1 {
		t.Fatalf("CreateVM вызван %d раз", len(cloud.creates))
	}
	req := cloud.creates[0]
	if req.ImageName != "OpensearchDashboards" {
		t.Errorf("image_name = %q, ждали имя из каталога", req.ImageName)
	}
	if req.ImageMetadata["opensearch_host"] != "db.internal" || req.ImageMetadata["hostname"] != "web-1" {
		t.Errorf("image_metadata = %v", req.ImageMetadata)
	}
	var got []string
	for _, r := range cloud.rules {
		got = append(got, r.IPProtocol+" "+r.PortRange)
	}
	if strings.Join(got, ";") != "tcp 80:80;tcp 8080:8080;udp 53:53" {
		t.Errorf("правила: %v", got)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("не JSON: %v\n%s", err, out)
	}
	ports, _ := res["ports"].(map[string]any)
	if ports == nil || len(ports["opened"].([]any)) != 3 {
		t.Errorf("ports в ответе: %v", res["ports"])
	}
	rec := stateRecord(t, vcVM)
	if rec == nil || rec.SecurityGroupID != vcGroup || len(rec.OpenedRules) != 3 {
		t.Errorf("state не знает правил для destroy: %+v", rec)
	}

	cloud.creates = nil
	_, errOut, code = execCLI(t, "--yes", "vm", "create", "astra", "--image", "astralinuxsmolensk1.8.3kes", "--no-public-ip")
	if code != 0 || len(cloud.creates) != 1 || cloud.creates[0].ImageName != "AstraLinuxSmolensk1.8.3KES" {
		t.Errorf("Astra Linux: exit %d, creates %+v\n%s", code, cloud.creates, errOut)
	}
}

func TestVMCreate_ImageRefusals(t *testing.T) {
	russian(t)
	cases := []struct {
		name string
		args []string
		code int
		want []string
	}{
		{"неоднозначно", []string{"--image", "astra"}, clerrors.ExitGeneral, []string{
			"образ \"astra\" неоднозначен: совпадений — 2",
			"  alse-base-1-7-7 — Astra Linux SE «Базовый» 1.7.7 — marketplace",
			"evo image list astra",
		}},
		{"не найден", []string{"--image", "nosuch os"}, clerrors.ExitNotFound, []string{
			"образ \"nosuch os\" не найден", "evo image list nosuch",
		}},
		{"нет поля", []string{"--image", "OpensearchDashboards", "--meta", "admin=x", "--meta", "opensearch_host=h"}, clerrors.ExitGeneral, []string{
			"в образе нет поля метаданных \"admin\"",
			"Поля образа: name, public_key, linux_password, hostname, opensearch_host, opensearch_password.",
		}},
		{"флаг состояния", []string{"--image", "OpensearchDashboards", "--meta", "vmm_enabled=false"}, clerrors.ExitGeneral, []string{
			"служебный флаг состояния",
		}},
		{"плохая пара", []string{"--image", "OpensearchDashboards", "--meta", "opensearch_host"}, clerrors.ExitGeneral, []string{
			"--meta \"opensearch_host\": нужно поле=значение",
		}},
		{"не заполнено обязательное", []string{"--image", "OpensearchDashboards"}, clerrors.ExitGeneral, []string{
			"образу нужны значения, которые evo не заполнит сам: opensearch: opensearch_host",
			"--meta opensearch_host=<значение>",
		}},
		{"плохой порт", []string{"--image", "OpensearchDashboards", "--port", "80/sctp"}, clerrors.ExitGeneral, []string{
			"неверный порт \"80/sctp\"",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupCLI(t)
			cloud := withVMCreateCloud(t)
			args := append([]string{"--yes", "vm", "create", "web"}, tc.args...)
			code, _, errOut := runCLI(t, "", args...)
			if code != tc.code {
				t.Fatalf("exit %d, ждали %d:\n%s", code, tc.code, errOut)
			}
			for _, w := range tc.want {
				if !strings.Contains(errOut, w) {
					t.Errorf("нет %q в:\n%s", w, errOut)
				}
			}
			noMutations(t, cloud)
		})
	}
}

// Превью показывает порты и поля, которые заданы, но не их значения.
func TestVMCreate_PreviewShowsPortsAndFieldNames(t *testing.T) {
	russian(t)
	setupCLI(t)
	withVMCreateCloud(t)
	code, out, errOut := runCLI(t, "", "--dry-run", "vm", "create", "web", "--image", "OpensearchDashboards",
		"--meta", "opensearch_password=s3cret-value", "--port", "443")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, errOut)
	}
	errOut += out
	t.Logf("\n%s", errOut)
	assertHas(t, errOut, []string{
		"  образ:      OpensearchDashboards (" + imgID(5) + ")\n",
		"  порты:      443 открываются для 0.0.0.0/0 в группе безопасности ВМ",
		"  поля:       opensearch_password — поля образа с заданными значениями\n",
	})
	if strings.Contains(errOut, "s3cret-value") {
		t.Errorf("значение поля в превью:\n%s", errOut)
	}
}

// stateRecord is the record of vmID in the state of the current directory
// (the one execCLI ran in).
func stateRecord(t *testing.T, vmID string) *state.DeploymentRecord {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return sf.FindDeployment(vmID)
}
