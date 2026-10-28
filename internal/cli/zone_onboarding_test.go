// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Догфуд 1.2.0: профиль от обычного 'evo configure' (зону он не спрашивал),
// пример из справки 'evo vm create my-vm --image Ubuntu-24.04 --flavor
// lowcost10-2-4 --port 80 --port 443'. Превью писало «не задано: зона»,
// всё равно спрашивало «Продолжить?», после «y» — отказ текстом для MCP
// («у сервера не задан CLOUDRU_REGION») и подсказка 'evo tools describe
// provision'; зона без флага — «нужно <name>, передано аргументов: 3».
// Все ID ниже выдуманы.

const (
	zoneAZ1 = "00000000-0000-4000-8000-0000000000a1"
	zoneAZ2 = "00000000-0000-4000-8000-0000000000a2"
	zoneAZ3 = "00000000-0000-4000-8000-0000000000a3"
)

// zoneList: ru.AZ-1 и ru.AZ-2 открыты, ru.AZ-3 закрыта для новых ресурсов.
const zoneList = `[{"id":"` + zoneAZ1 + `","name":"ru.AZ-1","enabled":true},` +
	`{"id":"` + zoneAZ2 + `","name":"ru.AZ-2","enabled":true},` +
	`{"id":"` + zoneAZ3 + `","name":"ru.AZ-3","enabled":false}]`

// Интерактивный configure спрашивает зону после проекта — как evo init:
// открытые зоны (закрытая названа в предупреждении), ru.AZ-2 первой и по
// умолчанию; Enter берёт показанную, ответ — номер, имя или ID; в профиль
// пишется UUID. Повтор предлагает текущую зону.
func TestConfigure_InteractiveAsksZone(t *testing.T) {
	fc := setupCLI(t)
	fc.zones = zoneList
	stdinIsTerminal = func() bool { return true }
	readPassword = func() ([]byte, error) { return []byte(fakeSecret), nil }

	code, stdout, stderr := runCLI(t, fakeKeyID+"\n\n", "configure")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	prof, _, _, _ := config.LoadProfile("default")
	if prof.Region != zoneAZ2 || prof.ProjectID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("profile: project %q, region %q; want ru.AZ-2 (%s)", prof.ProjectID, prof.Region, zoneAZ2)
	}
	az2, az1 := strings.Index(stderr, " 1) ru.AZ-2"), strings.Index(stderr, " 2) ru.AZ-1")
	if az2 < 0 || az1 < 0 || strings.Contains(stderr, ") ru.AZ-3") || !strings.Contains(stderr, "zone ru.AZ-3 is closed for new resources") ||
		!strings.Contains(stderr, "Choose a zone (number, name or ID) [1]: ") {
		t.Errorf("zone question:\n%s", stderr)
	}

	// A name, in any case, a wrong answer asked again.
	readPassword = func() ([]byte, error) { return nil, nil }
	code, _, stderr = runCLI(t, "\nru.AZ-9\nRU.az-1\n", "configure")
	if prof, _, _, _ = config.LoadProfile("default"); code != 0 || prof.Region != zoneAZ1 || !strings.Contains(stderr, "Not in the list") {
		t.Errorf("by name: exit %d, region %q:\n%s", code, prof.Region, stderr)
	}
	// Re-running offers the current zone: Enter keeps it.
	code, _, stderr = runCLI(t, "\n\n", "configure")
	if prof, _, _, _ = config.LoadProfile("default"); code != 0 || prof.Region != zoneAZ1 || !strings.Contains(stderr, " * 2) ru.AZ-1") ||
		!strings.Contains(stderr, "[2]: ") {
		t.Errorf("rerun: exit %d, region %q:\n%s", code, prof.Region, stderr)
	}
}

// Подсеть у нового проекта есть только в ru.AZ-1 (Default_ru.AZ-1): её зона
// предлагается первой, как в evo init, — ВМ в зоне без подсети не создать.
func TestConfigure_InteractiveZoneWithSubnetFirst(t *testing.T) {
	fc := setupCLI(t)
	fc.zones = zoneList
	fc.subnets = `[{"id":"00000000-0000-4000-8000-0000000000b1","name":"Default_ru.AZ-1","availability_zone_id":"` + zoneAZ1 + `","default":true}]`
	stdinIsTerminal = func() bool { return true }
	readPassword = func() ([]byte, error) { return []byte(fakeSecret), nil }
	code, stdout, stderr := runCLI(t, fakeKeyID+"\n\n", "configure")
	prof, _, _, _ := config.LoadProfile("default")
	if code != 0 || prof.Region != zoneAZ1 || !strings.Contains(stderr, " 1) ru.AZ-1") || !strings.Contains(stderr, "ru.AZ-2 is not offered first") {
		t.Errorf("exit %d, region %q: %s%s", code, prof.Region, stdout, stderr)
	}
}

// Список зон не прочитан (сеть, роль ключа): шаг пропускается одной
// строкой о том, как задать зону позже, ключ и проект сохраняются, код 0.
func TestConfigure_InteractiveZoneListFails(t *testing.T) {
	fc := setupCLI(t) // fc.zones "": the zone list answers 404
	stdinIsTerminal = func() bool { return true }
	readPassword = func() ([]byte, error) { return []byte(fakeSecret), nil }
	code, stdout, stderr := runCLI(t, fakeKeyID+"\n", "configure")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	prof, creds, _, _ := config.LoadProfile("default")
	if prof.Region != "" || prof.ProjectID == "" || creds.Secret != fakeSecret {
		t.Errorf("profile: %+v", prof)
	}
	// The API's 404 stays out of the answer; the profile's path is left
	// out of the search, as its random temp name may hold "404".
	var said []string
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "Configuring profile") {
			said = append(said, line)
		}
	}
	if fc.zoneCalls.Load() == 0 || !strings.Contains(stderr, "The zone list could not be read; set the zone later: evo configure --region <zone>") ||
		strings.Contains(strings.Join(said, "\n"), "404") {
		t.Errorf("skip line:\n%s", stderr)
	}
}

// --region без вопросов: имя зоны разрешается по списку в UUID, как у
// evo vm create; неизвестное имя — USAGE со списком зон, профиль не
// меняется; UUID записывается как есть, без чтения списка (как раньше).
func TestConfigure_RegionByName(t *testing.T) {
	fc := setupCLI(t)
	fc.zones = zoneList
	saveProfile(t, "default", "11111111-1111-1111-1111-111111111111")

	code, _, stderr := runCLI(t, "", "configure", "--region", "ru.az-2")
	if prof, _, _, _ := config.LoadProfile("default"); code != 0 || prof.Region != zoneAZ2 {
		t.Fatalf("by name: exit %d, region %q: %s", code, prof.Region, stderr)
	}

	code, _, stderr = runCLI(t, "", "--json", "configure", "--region", "ru.AZ-9")
	if prof, _, _, _ := config.LoadProfile("default"); code != clerrors.ExitGeneral || prof.Region != zoneAZ2 ||
		!strings.Contains(stderr, `"reason":"USAGE"`) || !strings.Contains(stderr, "ru.AZ-1, ru.AZ-2") {
		t.Errorf("unknown name: exit %d, region %q: %s", code, prof.Region, stderr)
	}

	before := fc.zoneCalls.Load()
	code, _, stderr = runCLI(t, "", "configure", "--region", zoneAZ1)
	if prof, _, _, _ := config.LoadProfile("default"); code != 0 || prof.Region != zoneAZ1 || fc.zoneCalls.Load() != before {
		t.Errorf("UUID: exit %d, region %q, zone reads %d: %s", code, prof.Region, fc.zoneCalls.Load()-before, stderr)
	}
}

// exampleCloud — previewCloud с образом примера из справки.
type exampleCloud struct{ previewCloud }

func (exampleCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return []*provision.Image{{ID: pvImage, Name: "Ubuntu-24.04"}}, nil
}
func (exampleCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return []*provision.Image{{ID: pvImage, Name: "Ubuntu-24.04"}}, nil
}

// execTree runs the CLI as execCLI does and returns the error itself, with
// its hint (nil without an error).
func execTree(t *testing.T, args ...string) (string, string, *clerrors.CLIError) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	oldTTY := confirm.StdinIsTTY
	confirm.StdinIsTTY = func() bool { return false }
	t.Cleanup(func() { confirm.StdinIsTTY = oldTTY })
	root := newRootCmd()
	root.SetArgs(args)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	_, err := root.ExecuteContextC(context.Background())
	var ce *clerrors.CLIError
	if err != nil && !errors.As(err, &ce) {
		t.Fatalf("not a CLIError: %v", err)
	}
	return out.String(), errOut.String(), ce
}

// Без зоны evo vm create и evo tools call provision (как MCP provision)
// отказывают до превью и до вопроса: тот же reason и код выхода (2,
// CONFIG_INVALID — зона, как проект, это настройка), без
// confirmation_required. Подсказка и сообщение — слова своей поверхности:
// команда называет свой флаг и evo configure --region и не называет
// инструмент MCP (догфуд 28.10: «provision: не задана зона…»), tools call
// — аргумент инструмента и его имя.
func TestVMCreate_NoZoneRefusedBeforeConsent(t *testing.T) {
	russian(t)
	withCloud(t, exampleCloud{})
	t.Setenv("CLOUDRU_REGION", "")
	example := []string{"vm", "create", "my-vm", "--image", "Ubuntu-24.04", "--flavor", "lowcost10-2-4", "--port", "80", "--port", "443"}

	_, errOut, ce := execTree(t, example...)
	if ce == nil || ce.Code != clerrors.CodeConfigInvalid || clerrors.ExitCode(ce) != clerrors.ExitAuth ||
		ce.Message != "не задана зона доступности (region)" ||
		ce.Hint != "Задайте --region (список: evo vm zones) или evo configure --region <зона>." {
		t.Fatalf("vm create: %+v\n%s", ce, errOut)
	}
	if strings.Contains(errOut, "План") || strings.Contains(errOut, "Продолжить") || strings.Contains(errOut, "подтверждение") {
		t.Errorf("asked for consent before the refusal:\n%s", errOut)
	}

	_, _, call := execTree(t, "tools", "call", "provision", "--arg", "name=my-vm", "--arg", "image=Ubuntu-24.04")
	if call == nil || call.Code != ce.Code || call.Message != "provision: "+ce.Message ||
		!strings.Contains(call.Hint, "list_zones") || !strings.Contains(call.Hint, "CLOUDRU_REGION") {
		t.Errorf("tools call: %+v", call)
	}

	// Without an image: USAGE (exit 1), the command's own flag.
	t.Setenv("CLOUDRU_IMAGE_ID", "")
	_, _, ce = execTree(t, "vm", "create", "my-vm", "--region", "ru.AZ-1")
	if ce == nil || ce.Code != clerrors.CodeUsage || ce.Message != "не задан образ (image или image_id)" ||
		ce.Hint != "Задайте --image (список: evo image list), например --image Ubuntu-24.04." {
		t.Errorf("no image: %+v", ce)
	}
}

// Пример из справки на профиле с зоной — превью и согласие, как раньше;
// зона именем в --region — та же зона по UUID.
func TestVMCreate_HelpExampleWithZone(t *testing.T) {
	russian(t)
	withCloud(t, exampleCloud{})
	example := []string{"vm", "create", "my-vm", "--image", "Ubuntu-24.04", "--flavor", "lowcost10-2-4", "--port", "80", "--port", "443", "--dry-run"}
	for name, region := range map[string]string{"зона профиля": "", "--region по имени": "ru.AZ-1"} {
		args := example
		t.Setenv("CLOUDRU_REGION", pvZone)
		if region != "" {
			t.Setenv("CLOUDRU_REGION", "")
			args = append(append([]string{}, example...), "--region", region)
		}
		out, errOut, code := execCLI(t, args...)
		if code != 0 || !strings.Contains(out, "зона:       ru.AZ-1 ("+pvZone+")") || !strings.Contains(out, "образ:      Ubuntu") {
			t.Errorf("%s: exit %d:\n%s%s", name, code, out, errOut)
		}
	}
}

// Отказ USAGE без своей подсказки: команда отсылает к своей справке,
// evo tools call — к описанию инструмента.
func TestToolError_UsageHintNamesTheCaller(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"vm", "create"})
	if err != nil {
		t.Fatal(err)
	}
	const text = `{"status":"error","reason":"USAGE","error":"bad argument"}`
	var ce *clerrors.CLIError
	if !errors.As(toolError(text, commandHints(cmd, nil)), &ce) || ce.Hint != "Check the arguments: 'evo vm create --help'." {
		t.Errorf("command: %+v", ce)
	}
	if !errors.As(toolError(text, callHints("provision")), &ce) || ce.Hint != "Check the arguments: 'evo tools describe provision'." {
		t.Errorf("tools call: %+v", ce)
	}
}

// Значение без флага — лишний аргумент: ошибка называет лишние аргументы
// и спрашивает, не пропущен ли флаг.
func TestTreeArgs_ExtraArgumentsNamed(t *testing.T) {
	russian(t)
	_, errOut, code := execCLI(t, "vm", "create", "my-vm", "UUID:", "00000000-0000-4000-8000-0000000000a1-long-value")
	want := `evo vm create: нужно <name>, передано аргументов: 3; лишние аргументы: "UUID:", "00000000-0000-4000-8000…" — пропущен флаг?`
	if code != clerrors.ExitGeneral || !strings.Contains(errOut, want) {
		t.Errorf("exit %d:\n%s\nwant %s", code, errOut, want)
	}
}

// Догфуд 28.10: отказы, видные без записи, приходят одним ответом. CLI в
// тексте называет каждый — первый как ошибку (её код выхода), остальные
// строками ниже, у каждого своя подсказка; в --json они в details.refusals.
func TestVMCreate_AllRefusalsShown(t *testing.T) {
	russian(t)
	withCloud(t, exampleCloud{})
	t.Setenv("CLOUDRU_REGION", "")
	t.Setenv("CLOUDRU_IMAGE_ID", "")
	stdinIsTerminal = func() bool { return false }
	_, errOut, ce := execTree(t, "vm", "create", "my-vm")
	if ce == nil || ce.Code != clerrors.CodeUsage || !strings.HasSuffix(ce.Message, "не задан образ (image или image_id)") {
		t.Fatalf("vm create: %+v\n%s", ce, errOut)
	}
	var out, stderr bytes.Buffer
	handleError(output.NewWithWriters(&out, &stderr, output.ModePlain, false), ce)
	text := stderr.String()
	if !strings.Contains(text, "не задан образ") || !strings.Contains(text, "не задана зона доступности") ||
		!strings.Contains(text, "--image") || !strings.Contains(text, "--region") {
		t.Errorf("text: want both refusals and the hint of each flag:\n%s", text)
	}
}

// Догфуд 28.10: evo vm create без зоны в терминале падал отказом. Теперь
// человек у терминала получает вопрос evo configure (открытые зоны, ru.AZ-2
// первой и по умолчанию; закрытая не предлагается), выбранная зона идёт в
// вызов и — по «да» — в профиль. Без терминала и с --yes — прежний отказ CONFIG_INVALID: зона не
// выбирается молча (D15, D48), профиль не меняется.
func TestVMCreate_NoZoneAskedAtTerminal(t *testing.T) {
	fc := setupCLI(t)
	// The tool's engine is the example world (no network), whose subnet is
	// in pvZone; the zone question reads the zones as configure does, from
	// the fake compute: pvZone open, ru.AZ-3 closed.
	fc.zones = `[{"id":"` + pvZone + `","name":"ru.AZ-1","enabled":true},` +
		`{"id":"` + zoneAZ3 + `","name":"ru.AZ-3","enabled":false}]`
	withCloud(t, exampleCloud{})
	t.Setenv("CLOUDRU_REGION", "")
	saveProfile(t, "default", "11111111-1111-1111-1111-111111111111")
	create := []string{"vm", "create", "my-vm", "--image", "Ubuntu-24.04", "--flavor", "lowcost10-2-4", "--dry-run"}

	for name, c := range map[string]struct {
		tty  bool
		args []string
	}{
		"no terminal": {false, create},
		"--yes":       {true, append([]string{"--yes"}, create...)},
	} {
		stdinIsTerminal = func() bool { return c.tty }
		code, _, stderr := runCLI(t, "\n\n", c.args...)
		prof, _, _, _ := config.LoadProfile("default")
		if code != clerrors.ExitAuth || strings.Contains(stderr, "Choose a zone") || prof.Region != "" {
			t.Errorf("%s: exit %d, region %q, want the refusal (2) and no question:\n%s", name, code, prof.Region, stderr)
		}
	}

	stdinIsTerminal = func() bool { return true }
	code, stdout, stderr := runCLI(t, "\n\n", create...)
	prof, _, _, _ := config.LoadProfile("default")
	if code != 0 || !strings.Contains(stdout, pvZone) {
		t.Errorf("terminal: exit %d, want the preview in the zone chosen:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, " 1) ru.AZ-1") || !strings.Contains(stderr, "Choose a zone (number, name or ID) [1]: ") ||
		!strings.Contains(stderr, "Keep this zone in profile default") || prof.Region != pvZone {
		t.Errorf("terminal: region %q, want ru.AZ-1 (%s) asked and saved:\n%s", prof.Region, pvZone, stderr)
	}
	if strings.Contains(stderr, "no availability zone is set") {
		t.Errorf("terminal: the zone chosen did not reach the call:\n%s", stderr)
	}
}

// The question of the zone is a tree command's (vm create, deploy): evo
// tools call is the CLI's MCP call, and at a terminal too it answers what
// MCP answers — the refusal, no question, no profile written (CLAUDE.md
// §1: the answer of the command is the tool's; release plan 1.5, a new
// surface, §0 rule 3).
func TestToolsCall_NoZoneNotAskedEvenAtTerminal(t *testing.T) {
	fc := setupCLI(t)
	fc.zones = `[{"id":"` + pvZone + `","name":"ru.AZ-1","enabled":true}]`
	withCloud(t, exampleCloud{})
	t.Setenv("CLOUDRU_REGION", "")
	saveProfile(t, "default", "11111111-1111-1111-1111-111111111111")
	call := []string{"tools", "call", "provision", "--arg", "name=my-vm", "--arg", "image=Ubuntu-24.04", "--arg", "flavor=lowcost10-2-4"}
	stdinIsTerminal = func() bool { return false }
	want, _, wantErr := runCLI(t, "", call...)
	stdinIsTerminal = func() bool { return true }
	code, _, stderr := runCLI(t, "\n\n", call...)
	prof, _, _, _ := config.LoadProfile("default")
	if code != want || strings.Contains(stderr, "Choose a zone") || prof.Region != "" {
		t.Errorf("terminal: exit %d (without one %d), region %q — want the same refusal and no question:\n%s", code, want, prof.Region, stderr)
	}
	if want == 0 || !strings.Contains(wantErr, "zone") && !strings.Contains(wantErr, "зон") {
		t.Fatalf("without a terminal: exit %d, want the refusal for the zone:\n%s", want, wantErr)
	}
}

// loginCloud is exampleCloud whose image declares its login user, as a
// Cloud.ru Linux image does: deploy gets past the image to the zone.
type loginCloud struct{ exampleCloud }

func (loginCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: id, Name: "Ubuntu-24.04", MinDisk: 20, MetadataFields: []provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"}}}, nil
}

// evo deploy is the second command that asks for the zone (callEngineTool,
// askMissingZone): at a terminal the question, the zone chosen in the
// profile and the call again with it; without a terminal and with --yes
// the refusal, no question, the profile untouched (D15, D48). The world
// has no subnet, so deploy cannot take a zone from one, and the call again
// stops at «no subnet for zone ru.AZ-1» — the zone chosen reached it.
// Release plan 1.5, a new surface (§0, rule 3): vm create was tested, the
// deploy path was not.
func TestDeploy_NoZoneAskedAtTerminal(t *testing.T) {
	fc := setupCLI(t)
	fc.zones = `[{"id":"` + pvZone + `","name":"ru.AZ-1","enabled":true}]`
	withCloud(t, noSubnetCloud{loginCloud{exampleCloud{}}})
	t.Setenv("CLOUDRU_REGION", "")
	t.Setenv("CLOUDRU_IMAGE_ID", "")
	saveProfile(t, "default", "11111111-1111-1111-1111-111111111111")
	if err := os.WriteFile("docker-compose.yml", []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".evo.yaml", []byte("image: Ubuntu-24.04\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		tty  bool
		args []string
	}{
		"no terminal": {false, []string{"deploy", "--dry-run"}},
		"--yes":       {true, []string{"--yes", "deploy", "--dry-run"}},
	} {
		newProcess()
		stdinIsTerminal = func() bool { return c.tty }
		code, _, stderr := runCLI(t, "\n\n", c.args...)
		prof, _, _, _ := config.LoadProfile("default")
		if code != clerrors.ExitAuth || strings.Contains(stderr, "Choose a zone") || prof.Region != "" ||
			!strings.Contains(stderr, "no availability zone is set") {
			t.Errorf("%s: exit %d, region %q, want the refusal for the zone (2) and no question:\n%s", name, code, prof.Region, stderr)
		}
	}
	newProcess()
	stdinIsTerminal = func() bool { return true }
	_, _, stderr := runCLI(t, "\n\n", "deploy", "--dry-run")
	prof, _, _, _ := config.LoadProfile("default")
	if !strings.Contains(stderr, "Choose a zone (number, name or ID) [1]: ") || prof.Region != pvZone {
		t.Errorf("terminal: region %q, want ru.AZ-1 (%s) asked and saved:\n%s", prof.Region, pvZone, stderr)
	}
	if strings.Contains(stderr, "no availability zone is set") || !strings.Contains(stderr, "no subnet for zone ru.AZ-1") {
		t.Errorf("terminal: the zone chosen did not reach the call again:\n%s", stderr)
	}
}
