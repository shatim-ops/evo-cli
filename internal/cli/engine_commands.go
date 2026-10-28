// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The engine commands — evo deploy, status, logs, destroy, list-zones,
// list-images, project list, whoami, doctor — predate the registry. They
// keep the help and the flags they had, and they call their tools (spec
// REQ-4, REQ-11): the work, the checks, the preview and the answer are the
// tool's, as MCP gets them. What a command adds is how it shows them
// (tools.Caller): the engine's steps and prompts as they happen, the
// consent at the engine's gate, the table of status, the error as the
// engine gave it, doctor's checks of this machine.

// engineCommand is one engine command: the tool it calls, the tool argument
// each of its flags gives (Flags), the flags that give none (CLIOnly) and
// the arguments it has no flag for (Omit), each with the reason. Alias,
// when set, says why the command is a second command of its tool: the
// tool's own command is the one its CLISpec names. TestParity_EngineCommands
// keeps the table, the commands and the schemas in step.
type engineCommand struct {
	Path    string
	Tool    string
	Alias   string
	Flags   map[string]string
	CLIOnly map[string]string
	Omit    map[string]string
}

var engineCommands = []engineCommand{
	{Path: "evo deploy", Tool: "deploy",
		Flags: map[string]string{"compose-file": "compose_file", "flavor": "flavor", "region": "region", "project-id": "project_id",
			"image-id": "image_id", "image": "image", "disk-size": "disk_size", "force": "force"},
		CLIOnly: map[string]string{"non-interactive": "не спрашивать на терминале ни согласия, ни об удалении ВМ после Ctrl-C: у MCP нет терминала, согласие в нём — confirmed"}},
	{Path: "evo status", Tool: "status",
		// --no-refresh is refresh=false.
		Flags: map[string]string{"no-refresh": "refresh"}},
	{Path: "evo logs", Tool: "logs",
		Flags:   map[string]string{"service": "service", "tail": "tail"},
		CLIOnly: map[string]string{"follow": "журнал потоком до Ctrl-C: потоковых ответов в MCP нет (spec.md, не-цели; Q7)"}},
	{Path: "evo destroy", Tool: "destroy",
		Alias:   "удаляет ВМ развёртывания этого каталога (destroy без vm_id и name); ВМ по ID или имени удаляет evo vm delete",
		CLIOnly: map[string]string{"force": "устаревший синоним --yes, скрыт из справки; удаляется в v2.0"},
		Omit: map[string]string{"vm_id": "ВМ по ID удаляет evo vm delete", "name": "ВМ по имени удаляет evo vm delete",
			"release_ip": "destroy без vm_id и name освобождает floating IP развёртывания всегда"}},
	{Path: "evo list-zones", Tool: "list_zones",
		Alias: "скрытый устаревший алиас evo vm zones: его --json — массив зон, а не ответ инструмента; удаляется в v2.0"},
	{Path: "evo list-images", Tool: "list_images"},
	{Path: "evo project list", Tool: "org_projects",
		Alias: "алиас evo org projects; до v2.0 ответ Organization API с ошибкой — пометка, а не ошибка, как с v1.0"},
	{Path: "evo whoami", Tool: "whoami"},
	{Path: "evo doctor", Tool: "doctor"},
}

// engineToolAt is the tool of the engine command at path, "" if none.
func engineToolAt(path string) string {
	for _, c := range engineCommands {
		if c.Path == path && c.Alias == "" {
			return c.Tool
		}
	}
	return ""
}

// engineCommandOf is the engine command that is the tool's own command
// (no alias), if there is one: the trees do not build it from the schema.
func engineCommandOf(tool string) (engineCommand, bool) {
	for _, c := range engineCommands {
		if c.Tool == tool && c.Alias == "" {
			return c, true
		}
	}
	return engineCommand{}, false
}

// engineCmd marks cmd as the command of the engine tool: the annotation of
// the trees, for TestParity_CommandsAndTools.
func engineCmd(cmd *cobra.Command, tool string) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[toolAnnotation] = tool
	return cmd
}

// callEngineTool calls the tool of an engine command as the command c. The
// error is the engine's own (Caller.Err) when the tool reports one, else
// the tool's error answer as every command shows it (toolError).
func callEngineTool(cmd *cobra.Command, tool string, args map[string]any, c *tools.Caller) (string, error) {
	t, ok := newRegistry().Lookup(tool)
	if !ok {
		return "", fmt.Errorf("no tool %s", tool)
	}
	text, isErr := t.Handler(tools.WithCaller(cmd.Context(), c), args)
	if isErr {
		// evo deploy at a terminal asks for the zone it lacks, as evo vm
		// create does (askMissingZone), and runs again with it.
		zone, ok, err := askMissingZone(cmd.Context(), cmd, text)
		if err != nil {
			return "", err
		}
		if ok {
			args["region"] = zone
			c.Err = nil
			text, isErr = t.Handler(tools.WithCaller(cmd.Context(), c), args)
		}
	}
	if !isErr {
		return text, nil
	}
	if c.Err != nil {
		return text, withAccessDenied(c.Err, text)
	}
	return text, toolError(tool, text, commandHints(cmd, nil))
}

// withAccessDenied is err, the engine's error of the command, with the
// access_denied and http_status of the tool's answer text in its details:
// a refusal of rights says so as the answer of any other command does
// (toolError, answerDetails).
func withAccessDenied(err error, text string) error {
	m := parseObject(text)
	var ce *clerrors.CLIError
	if m["access_denied"] != true || !stderrors.As(err, &ce) {
		return err
	}
	details := make(map[string]any, len(ce.Details)+2)
	for k, v := range ce.Details {
		details[k] = v
	}
	details["access_denied"], details["http_status"] = m["access_denied"], m["http_status"]
	ce.Details = details // the call's own error: the message and its context stay
	return err
}

// engineGate is the consent of an engine command (tools.Caller.Gate): the
// preview, in the engine's words when it gives them (human: destroy, an
// update), else as the CLI shows the preview of a tool (a new VM), with the
// tool's answer as its details — as 'evo tools call' and MCP get it
// (spec REQ-6) — through confirm.Ask: a prompt, --yes, --dry-run.
func engineGate(cmd *cobra.Command, tool string, yes, noPrompt bool) func(context.Context, string, *confirm.Preview) (bool, error) {
	return func(ctx context.Context, answer string, human *confirm.Preview) (bool, error) {
		var p confirm.Preview
		if human != nil {
			p = *human
		} else {
			resp := parseObject(answer)
			p = toolPreview(tool, resp)
			if cp, ok := createPreview(tool, resp); ok {
				p = cp
			}
		}
		// The answer as the tool wrote it, its key order included.
		p.Details = json.RawMessage(answer)
		return confirm.Ask(ctx, p, confirm.Options{Yes: yes, DryRun: flagDryRun, NoPrompt: noPrompt, JSON: flagJSON,
			Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()})
	}
}

// --- deploy ---

func newDeployCmd() *cobra.Command {
	var (
		composeFile    string
		flavor         string
		region         string
		projectID      string
		imageID        string
		image          string
		diskSize       int
		force          bool
		nonInteractive bool
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: i18n.T("Deploy a docker-compose project to a Cloud.ru VM", "Развернуть docker-compose проект на ВМ Cloud.ru"),
		Long: i18n.T(`Deploy a docker-compose project to a Cloud.ru VM.

The first run creates a VM with a floating IP, opens the compose file's ports
in the zone's security group, installs Docker, uploads the compose file and
runs 'docker compose up -d'. Before the VM is created it shows a preview with
the price (the Cloud.ru price calculation, else evo's estimate) and asks for
confirmation: a question on the terminal, --yes to agree in advance,
--dry-run for the preview only. A repeated run updates the same VM if the
compose file changed (with --force, even if it did not).`, `Развернуть docker-compose проект на ВМ Cloud.ru.

Первый запуск создаёт ВМ с floating IP, открывает порты из compose-файла в
группе безопасности зоны, ставит Docker, загружает compose-файл и выполняет
'docker compose up -d'. Перед созданием ВМ показывается превью с ценой
(расчёт Cloud.ru, иначе оценка evo) и запрашивается подтверждение: вопрос на
терминале, --yes — согласие заранее, --dry-run — только превью. Повторный
запуск обновляет ту же ВМ, если compose-файл изменился (с --force — и без
изменений).`),
		Example: i18n.T(`  evo deploy --dry-run
  evo deploy --yes
  evo deploy -f compose.prod.yml --flavor lowcost10-2-4`, `  evo deploy --dry-run
  evo deploy --yes
  evo deploy -f compose.prod.yml --flavor lowcost10-2-4`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("image-id") && cmd.Flags().Changed("image") {
				return clerrors.New(clerrors.CodeUsage,
					i18n.T("--image and --image-id name the same thing", "--image и --image-id задают одно и то же"),
					i18n.T("Keep one: --image takes a name or a UUID.", "Оставьте один: --image принимает имя или UUID."))
			}
			values := map[string]any{"compose-file": composeFile, "flavor": flavor, "region": region, "project-id": projectID,
				"image-id": imageID, "image": image, "disk-size": float64(diskSize), "force": force}
			callArgs := engineArgs(cmd, "evo deploy", values)
			c := &tools.Caller{Printer: cmdPrinter(cmd), Gate: engineGate(cmd, "deploy", flagYes, nonInteractive), Yes: flagYes,
				DryRun: flagDryRun, NonInteractive: nonInteractive}
			_, err := callEngineTool(cmd, "deploy", callArgs, c)
			return err
		},
	}

	cmd.Flags().StringVarP(&composeFile, "compose-file", "f", "", i18n.T("Path to docker-compose file (default: docker-compose.yml)",
		"Путь к compose-файлу (по умолчанию docker-compose.yml)"))
	cmd.Flags().StringVar(&flavor, "flavor", "", i18n.T("VM flavor (default: auto)", "Flavor ВМ (по умолчанию auto — подбор по сервисам)"))
	cmd.Flags().StringVar(&region, "region", "", i18n.T("Availability zone: a name such as ru.AZ-2 or its UUID (evo vm zones)", "Зона доступности: имя (например, ru.AZ-2) или UUID (evo vm zones)"))
	cmd.Flags().StringVar(&projectID, "project-id", "", i18n.T("Cloud.ru project UUID", "UUID проекта Cloud.ru"))
	cmd.Flags().StringVar(&imageID, "image-id", "", i18n.T("Boot image UUID", "UUID загрузочного образа"))
	cmd.Flags().StringVar(&image, "image", "", i18n.T("Boot image: a name from 'evo image list' or a UUID (Ubuntu or Debian)",
		"Загрузочный образ: имя из 'evo image list' или UUID (Ubuntu или Debian)"))
	cmd.Flags().IntVar(&diskSize, "disk-size", 0, i18n.T("Boot disk size in GB (default: 10)", "Размер загрузочного диска, ГБ (по умолчанию 10)"))
	cmd.Flags().BoolVar(&force, "force", false, i18n.T("Force redeploy even if compose hash unchanged",
		"Развернуть заново, даже если compose-файл не изменился"))
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, i18n.T("Never prompt; creating a VM, and updating the VM of an existing deployment, then requires --yes",
		"Никогда не спрашивать; тогда и для создания ВМ, и для обновления ВМ развёртывания нужен --yes"))

	// evo deploy is an action and the group of the deploy wizard (plan,
	// apply, list: the trees add them) at once (spec.md R7): an argument
	// that is no subcommand ('evo deploy pln') is "unknown command", as it
	// was with cobra.NoArgs, now with a suggestion.
	cmd.SuggestionsMinimumDistance = 2
	cmd.Args = unknownSubcommand

	return engineCmd(cmd, "deploy")
}

// engineArgs are the tool arguments of the changed flags of the engine
// command at path: values holds each flag's value, as the tool takes it.
func engineArgs(cmd *cobra.Command, path string, values map[string]any) map[string]any {
	var ec engineCommand
	for _, c := range engineCommands {
		if c.Path == path {
			ec = c
		}
	}
	out := map[string]any{}
	for flag, arg := range ec.Flags {
		if v, ok := values[flag]; ok && cmd.Flags().Changed(flag) {
			out[arg] = v
		}
	}
	return out
}

// --- status ---

func newStatusCmd() *cobra.Command {
	var noRefresh bool
	cmd := &cobra.Command{
		Use:   "status",
		Args:  cobra.NoArgs,
		Short: i18n.T("Show current deployment status", "Состояние текущего развёртывания"),
		Long: i18n.T(`Show this directory's deployment from the local state, and the VM's live
state from one request to Cloud.ru (at most 8 s). A VM deleted outside evo
(in the console) shows as not found in the cloud: 'evo destroy' removes its
record. If the cloud cannot be asked (no key, network, API error), status
still shows the local state and says why the cloud was not checked. status
never changes the state.

--no-refresh: the local state only, no request. With --json the answer adds
cloud_checked, cloud_state, not_found_in_cloud, cloud_error and cloud_reason.`,
			`Показать развёртывание этого каталога по локальному state и живое состояние
ВМ — одним запросом к Cloud.ru (не дольше 8 с). ВМ, удалённая вне evo (в
консоли), показывается как не найденная в облаке: 'evo destroy' уберёт её
запись. Если облако спросить не удалось (нет ключа, сети, ошибка API),
status всё равно показывает локальное состояние и пишет, почему облако не
проверено. state команда не меняет.

--no-refresh — только локальный state, без запроса. С --json в ответе
добавляются cloud_checked, cloud_state, not_found_in_cloud, cloud_error и
cloud_reason.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --no-refresh is refresh=false; refresh is true by default.
			callArgs := engineArgs(cmd, "evo status", map[string]any{"no-refresh": !noRefresh})
			text, err := callEngineTool(cmd, "status", callArgs, &tools.Caller{})
			if err != nil {
				return err
			}
			p := cmdPrinter(cmd)
			if p.IsJSON() {
				fmt.Fprintln(cmd.OutOrStdout(), text)
				return nil
			}
			return showStatus(p, text, !noRefresh)
		},
	}
	cmd.Flags().BoolVar(&noRefresh, "no-refresh", false, i18n.T("Local state only: do not ask Cloud.ru for the VM's live state",
		"Только локальный state: не спрашивать Cloud.ru о живом состоянии ВМ"))
	return engineCmd(cmd, "status")
}

// showStatus is the answer of the status tool as 'evo status' shows it: the
// table of the deployment (with the VM's state in the cloud when it was
// asked: refreshed), or the note that there is none.
func showStatus(p *output.Printer, text string, refreshed bool) error {
	var probe struct {
		ID     *string `json:"id"`
		Status string  `json:"status"`
	}
	if err := json.Unmarshal([]byte(text), &probe); err != nil {
		return err
	}
	if probe.ID == nil && probe.Status == "none" {
		engine.ShowNoDeployment(p)
		return nil
	}
	var st engine.DeploymentStatus
	if err := json.Unmarshal([]byte(text), &st); err != nil {
		return err
	}
	engine.ShowStatus(p, st, refreshed)
	return nil
}

// --- logs ---

func newLogsCmd() *cobra.Command {
	var (
		service string
		tail    int
		follow  bool
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Args:  cobra.NoArgs,
		Short: i18n.T("View container logs from the deployed VM", "Журналы контейнеров на развёрнутой ВМ"),
		Example: i18n.T(`  evo logs
  evo logs -s web -n 50
  evo logs -f`, `  evo logs
  evo logs -s web -n 50
  evo logs -f`),
		RunE: func(cmd *cobra.Command, args []string) error {
			callArgs := map[string]any{"service": service, "tail": float64(tail)}
			// The log goes to the terminal as it comes; -f follows it.
			_, err := callEngineTool(cmd, "logs", callArgs, &tools.Caller{Printer: cmdPrinter(cmd), Follow: follow})
			return err
		},
	}

	cmd.Flags().StringVarP(&service, "service", "s", "", i18n.T("Filter logs by service name", "Только журнал этого сервиса"))
	cmd.Flags().IntVarP(&tail, "tail", "n", 100, i18n.T("Number of lines to show (0 = all)", "Сколько последних строк показать (0 — все)"))
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, i18n.T("Follow log output", "Следить за журналом (до Ctrl-C)"))

	return engineCmd(cmd, "logs")
}

// --- destroy ---

func newDestroyCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "destroy",
		Args:  cobra.NoArgs,
		Short: i18n.T("Delete the deployed VM and clean up resources", "Удалить развёрнутую ВМ и связанные ресурсы"),
		Long: i18n.T(`Delete this directory's deployment VM, release its floating IP and
remove the security group rules deploy created.

Before deleting it shows a preview and asks for confirmation: a question on
the terminal, --yes to agree in advance, --dry-run for the preview only. Data
on the VM's disks is lost.`, `Удалить ВМ развёртывания из этого каталога, освободить её floating IP и
удалить правила группы безопасности, которые создал deploy.

Перед удалением показывается превью и запрашивается подтверждение: вопрос на
терминале, --yes — согласие заранее, --dry-run — только превью.
Данные на дисках ВМ теряются.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if force {
				cmdPrinter(cmd).Warning(i18n.T("destroy --force is deprecated: use --yes",
					"destroy --force устарел: используйте --yes"))
			}
			c := &tools.Caller{Printer: cmdPrinter(cmd), Gate: engineGate(cmd, "destroy", force || flagYes, false), Yes: force || flagYes, DryRun: flagDryRun}
			_, err := callEngineTool(cmd, "destroy", map[string]any{}, c)
			return err
		},
	}

	// --force meant the same as --yes here, while deploy --force redeploys
	// an unchanged project: one word, two meanings. It keeps working for old
	// scripts, hidden from help, with a warning.
	cmd.Flags().BoolVar(&force, "force", false, i18n.T("Deprecated: use --yes", "Устарел: используйте --yes"))
	_ = cmd.Flags().MarkHidden("force")

	return engineCmd(cmd, "destroy")
}

// --- list-zones ---

// list-zones is the old form of 'evo vm zones': hidden, kept working with a
// one-line hint until v2.0, its --json the array of zones it always gave.
func newListZonesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "list-zones",
		Hidden: true,
		Args:   cobra.NoArgs,
		Short:  i18n.T("List available availability zones", "Список зон доступности"),
		Long: i18n.T(`List availability zones from the Cloud.ru API.

Pass the zone's name or ID as the --region flag or CLOUDRU_REGION env var for deploy.
Use --json for machine-readable output.`, `Список зон доступности из API Cloud.ru.

Имя или ID зоны передаётся в deploy флагом --region или переменной CLOUDRU_REGION.
Для машинного разбора — --json.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := cmdPrinter(cmd)
			p.Warning(i18n.T("evo list-zones is deprecated: use 'evo vm zones'",
				"evo list-zones устарела: используйте 'evo vm zones'"))
			text, err := callEngineTool(cmd, "list_zones", map[string]any{}, &tools.Caller{})
			if err != nil {
				return err
			}
			var answer struct {
				Zones []*provision.AvailabilityZone `json:"zones"`
			}
			if err := json.Unmarshal([]byte(text), &answer); err != nil {
				return err
			}
			zones := answer.Zones
			if p.IsJSON() {
				return p.JSON(zones)
			}

			headers := []string{"ID", i18n.T("Name", "Имя"), i18n.T("New resources", "Новые ресурсы")}
			rows := make([][]string, len(zones))
			for i, z := range zones {
				state := i18n.T("open", "открыта")
				if z.Closed() {
					state = i18n.T("closed (enabled=false)", "закрыта (enabled=false)")
				}
				rows[i] = []string{z.ID, z.Name, state}
			}
			p.Table(headers, rows)
			return nil
		},
	}
	return engineCmd(cmd, "list_zones")
}

// --- list-images ---

// list-images is the old form of 'evo image list' (review r5 U6): hidden,
// kept working, with a one-line hint. It is the command of the tool
// list_images, whose answer it shows as it always did: --json the array of
// images.
func newListImagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "list-images",
		Hidden: true,
		Args:   cobra.NoArgs,
		Short:  i18n.T("List available VM boot images", "Список загрузочных образов ВМ"),
		Long: i18n.T(`List boot images from the Cloud.ru API.

Use the image ID as the --image-id flag or CLOUDRU_IMAGE_ID env var for deploy.
deploy installs Docker on Ubuntu and Debian images (e.g. Ubuntu-24.04).
Use --json for machine-readable output.`, `Список загрузочных образов из API Cloud.ru.

ID образа передаётся в deploy флагом --image-id или переменной CLOUDRU_IMAGE_ID.
deploy ставит Docker на образы Ubuntu и Debian (например, Ubuntu-24.04).
Для машинного разбора — --json.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := cmdPrinter(cmd)
			p.Warning(i18n.T("evo list-images is deprecated: use 'evo image list [query]'",
				"evo list-images устарела: используйте 'evo image list [запрос]'"))
			text, err := callEngineTool(cmd, "list_images", map[string]any{}, &tools.Caller{})
			if err != nil {
				return err
			}
			var answer struct {
				Images  []*provision.Image `json:"images"`
				Warning string             `json:"warning"`
			}
			if err := json.Unmarshal([]byte(text), &answer); err != nil {
				return err
			}
			if answer.Warning != "" {
				p.Warning(answer.Warning)
			}
			images := answer.Images
			if len(images) == 0 {
				p.Success(i18n.T("No images found.", "Образы не найдены."))
				return nil
			}
			if p.IsJSON() {
				return p.JSON(images)
			}

			headers := []string{"ID", i18n.T("Name", "Имя"), i18n.T("OS", "ОС"), i18n.T("Status", "Состояние")}
			rows := make([][]string, 0, len(images))
			for _, img := range images {
				rows = append(rows, []string{img.ID, img.Name, img.OS, img.Status})
			}
			p.Table(headers, rows)
			return nil
		},
	}
	return engineCmd(cmd, "list_images")
}

// --- project list ---

func newProjectListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: i18n.T("List the projects the key sees (* — the active one)", "Проекты, которые видит ключ (* — активный)"),
		Example: `  evo project list
  evo project list --json | jq -r '.projects[].id'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			c := &tools.Caller{}
			text, err := callEngineTool(cmd, "org_projects", map[string]any{}, c)
			switch {
			case err == nil:
			case tools.OrganizationAnswered(c.Err):
				// The Organization API answered with an error: since v1.0
				// this command takes any such answer as "the key cannot
				// list projects" — a note and the active project, exit 0
				// (QA r3 C3); org_projects does so for 400, 403 and 404.
				// So until v2.0.
				cfg, cerr := resolveConfig(printer, scopeOverrides())
				if cerr != nil {
					return cerr
				}
				text = toJSONText(map[string]any{"projects": nil, "active": cfg.ProjectID, "note": tools.ScopeText(0, false)})
			case !isCLIError(c.Err):
				// No answer (the network): the tool's error, with its code.
				return toolError("org_projects", text, commandHints(cmd, nil))
			default:
				return err
			}
			if printer.IsJSON() {
				fmt.Fprintln(cmd.OutOrStdout(), text)
				return nil
			}
			return renderProjectList(printer, cmd.OutOrStdout(), text)
		},
	}
	return engineCmd(cmd, "org_projects")
}

// renderProjectList shows an answer of org_projects as 'evo project list'
// always has: a table with the active project marked "*", or the note that
// the key cannot list projects and the active project.
func renderProjectList(p *output.Printer, _ io.Writer, text string) error {
	var answer struct {
		Projects []orgProject `json:"projects"`
		Active   string       `json:"active"`
		Note     string       `json:"note"`
	}
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		return err
	}
	if answer.Projects == nil && answer.Note != "" {
		p.Warning(i18n.Tf("project list: %s", "список проектов: %s", answer.Note))
		if answer.Active != "" {
			p.Success(i18n.Tf("Active project: %s", "Активный проект: %s", answer.Active))
		}
		return nil
	}
	if len(answer.Projects) == 0 {
		p.Success(i18n.T("The key sees no projects.", "Ключ не видит ни одного проекта."))
		return nil
	}
	rows := make([][]string, len(answer.Projects))
	for i, pr := range answer.Projects {
		id := pr.ID
		if id == answer.Active {
			id += " *"
		}
		rows[i] = []string{id, pr.Name, pr.Status}
	}
	p.Table([]string{"ID", i18n.T("Name", "Имя"), i18n.T("Status", "Статус")}, rows)
	return nil
}

func toJSONText(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func isCLIError(err error) bool {
	var ce *clerrors.CLIError
	return stderrors.As(err, &ce)
}
