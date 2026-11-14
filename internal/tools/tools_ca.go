// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/containerapp"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// caClient builds an authenticated Container Apps client.
func (s *Registry) caClient(ctx context.Context) (*containerapp.Client, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return containerapp.New(tok.Value, eng.Config.ProjectID, ""), nil
}

func (s *Registry) handleCaList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.List(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{"name": m["name"], "status": m["status"], "id": m["id"]})
	}
	return toJSON(map[string]any{"containers": out}), false
}

func (s *Registry) handleCaGet(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageT("name is required", "нужен name"), true
	}
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.Get(ctx, name)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// caState is the status of the container app name as the API gives it.
func (s *Registry) caState(ctx context.Context, name string) (string, error) {
	cli, err := s.caClient(ctx)
	if err != nil {
		return "", err
	}
	m, err := cli.Get(ctx, name)
	if err != nil {
		return "", err
	}
	state, _ := m["status"].(string)
	return state, nil
}

// caNameRe is an RFC 1123 label: what the description of ca_deploy asks.
var caNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// caReservedPorts are the ports Container Apps reserves besides 1–1024
// (docs, guides__additionalports: «8008, 8012, 8013, 8112, 8022, 9090,
// 9091, 9092 и порты от 1 до 1024 зарезервированы»; container-domain-create
// lists the same without 9092, so 9092 is refused too).
var caReservedPorts = []int{8008, 8012, 8013, 8022, 8112, 9090, 9091, 9092}

// caDeployReq builds the create request of ca_deploy and checks what needs
// no API, before the confirmation gate: an RFC 1123 name, a port the
// platform does not reserve, 0 <= min_instances <= max_instances. The API
// answered 400 «Invalid argument» without naming the field (release audit,
// L-28).
func caDeployReq(args map[string]any) (containerapp.CreateContainerReq, error) {
	name := stringArg(args, "name")
	image := stringArg(args, "image")
	if name == "" || image == "" {
		return containerapp.CreateContainerReq{}, clierrors.New(clierrors.CodeUsage, i18n.T("name and image are required", "нужны name и image"), "")
	}
	if !caNameRe.MatchString(name) {
		return containerapp.CreateContainerReq{}, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("name %q is not an RFC 1123 label", "имя %q не по RFC 1123", name),
			i18n.T("Lowercase letters, digits and '-', 1-63 characters, starting and ending with a letter or digit, e.g. my-app.", "Строчные латинские буквы, цифры и '-', 1–63 символа, в начале и в конце — буква или цифра, например my-app."))
	}
	cpu := firstNonEmptyStr(stringArg(args, "cpu"), "0.1")
	memory := firstNonEmptyStr(stringArg(args, "memory"), "256Mi")
	port := intArg(args, "port", 8080)
	switch {
	case port < 1 || port > 65535:
		return containerapp.CreateContainerReq{}, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("port %d is out of range 1-65535", "порт %d вне диапазона 1–65535", port), i18n.T("The default is 8080.", "По умолчанию 8080."))
	case port <= 1024 || slices.Contains(caReservedPorts, port):
		// Ports up to 1024 used to switch the container to privileged mode
		// (root) without a word in the preview; the platform reserves them.
		return containerapp.CreateContainerReq{}, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("port %d is reserved by Container Apps", "порт %d занят Container Apps", port),
			i18n.T("Reserved: 1-1024, 8008, 8012, 8013, 8022, 8112, 9090, 9091, 9092. Pick another one, e.g. 8080 (the default); the app must listen on 0.0.0.0:$PORT.", "Заняты: 1–1024, 8008, 8012, 8013, 8022, 8112, 9090, 9091, 9092. Выберите другой, например 8080 (по умолчанию); приложение должно слушать 0.0.0.0:$PORT."))
	}
	public := true
	if v, ok := args["public"].(bool); ok {
		public = v
	}
	privileged, _ := args["privileged"].(bool)
	minI := intArg(args, "min_instances", 0)
	maxI := intArg(args, "max_instances", 1)
	if minI < 0 || maxI < 1 || minI > maxI {
		return containerapp.CreateContainerReq{}, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("min_instances %d and max_instances %d: need 0 <= min_instances <= max_instances and max_instances >= 1", "min_instances %d и max_instances %d: нужно 0 <= min_instances <= max_instances и max_instances >= 1", minI, maxI),
			i18n.T("The defaults are 0 (scale to zero) and 1.", "По умолчанию 0 (масштабирование до нуля) и 1."))
	}

	var env []containerapp.Env
	if raw, ok := args["env"].(map[string]any); ok {
		for k, v := range raw {
			env = append(env, containerapp.Env{Name: k, Value: fmt.Sprintf("%v", v)})
		}
		slices.SortFunc(env, func(a, b containerapp.Env) int { return strings.Compare(a.Name, b.Name) })
	}

	return containerapp.CreateContainerReq{
		Name:          name,
		Description:   stringArg(args, "description"),
		Configuration: &containerapp.Configuration{Ingress: &containerapp.Ingress{PubliclyAccessible: public}, Privileged: privileged},
		Template: containerapp.Template{
			Protocol: "http_1",
			Scaling:  &containerapp.Scaling{MinInstanceCount: minI, MaxInstanceCount: maxI},
			Containers: []containerapp.Container{{
				Name:          name,
				Image:         image,
				Resources:     &containerapp.Resources{CPU: cpu, Memory: memory},
				ContainerPort: port,
				Env:           env,
			}},
		},
	}, nil
}

// caDeployPreview is what ca_deploy will create: every setting of the
// request, env by names only (a value may be a secret), and an explicit
// line when the container runs as root.
func caDeployPreview(req containerapp.CreateContainerReq) map[string]any {
	c := req.Template.Containers[0]
	p := map[string]any{
		"name":          req.Name,
		"image":         c.Image,
		"port":          c.ContainerPort,
		"cpu":           c.Resources.CPU,
		"memory":        c.Resources.Memory,
		"protocol":      req.Template.Protocol,
		"public":        req.Configuration.Ingress.PubliclyAccessible,
		"min_instances": req.Template.Scaling.MinInstanceCount,
		"max_instances": req.Template.Scaling.MaxInstanceCount,
		"privileged":    req.Configuration.Privileged,
	}
	if req.Configuration.Privileged {
		p["runs_as"] = "root (privileged=true)"
	} else {
		p["runs_as"] = "UID 1000 (not privileged)"
	}
	if len(c.Env) > 0 {
		names := make([]string, 0, len(c.Env))
		for _, e := range c.Env {
			names = append(names, e.Name)
		}
		p["env"] = names
	}
	if req.Description != "" {
		p["description"] = req.Description
	}
	return p
}

// planCaDeploy plans ca_deploy, which creates and runs a container app
// (D35). Async.
func (s *Registry) planCaDeploy(ctx context.Context, args map[string]any) (*Plan, error) {
	req, err := caDeployReq(args)
	if err != nil {
		return nil, err
	}
	name := req.Name
	msg := i18n.Tf("ca_deploy will create+run container app %q from image %q (billable).",
		"ca_deploy создаст и запустит приложение %q из образа %q (платно).", name, req.Template.Containers[0].Image)
	if req.Configuration.Privileged {
		msg = i18n.Tf("ca_deploy will create+run container app %q from image %q in PRIVILEGED mode: the container runs as root (billable).",
			"ca_deploy создаст и запустит приложение %q из образа %q в ПРИВИЛЕГИРОВАННОМ режиме: контейнер работает от root (платно).", name, req.Template.Containers[0].Image)
	}
	will, warning := caDeployPreview(req), ""
	if req.Configuration.Privileged {
		warning = i18n.T("privileged mode: the container runs as root", "привилегированный режим: контейнер работает от root")
	}
	preview := map[string]any{"message": msg,
		"will_create": will,
		"preview": willPreview(engine.ResourceCAApp, name, "", will,
			[]string{"image", "port", "protocol", "cpu", "memory", "public", "min_instances", "max_instances", "runs_as", "env"}, warning),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		cli, err := s.caClient(ctx)
		if err != nil {
			return errResult(err), true
		}
		op, err := cli.Create(ctx, req)
		if err != nil || op.ID == "" {
			if err == nil {
				err = noID("operation")
			}
			return lostCreate(ctx, "ca_deploy", cli.List, i18n.T("container app", "приложение"), name, nil, "ca_get name", err)
		}
		return toJSON(map[string]any{
			"status":       "deploying",
			"name":         name,
			"operation_id": op.ID,
			"note":         "Container app is deploying (async). Use ca_get for status + public URL, ca_logs for logs. Scales to zero when idle.",
		}), false
	}}, nil
}

func (s *Registry) planCaDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	name := stringArg(args, "name")
	if name == "" {
		return nil, usageErr("name is required", "нужен name")
	}
	cli, err := s.caClient(ctx)
	if err != nil {
		return nil, err
	}
	// An app that is not there is already deleted (D26), not a question.
	if _, err := cli.Get(ctx, name); err != nil {
		return answered(s.deleteFailed("ca_delete", name, err)), nil
	}
	preview := map[string]any{"message": i18n.Tf("ca_delete permanently deletes container app %q.",
		"ca_delete безвозвратно удаляет приложение %q.", name),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		op, err := cli.Delete(ctx, name)
		if err != nil {
			return s.deleteFailed("ca_delete", name, err)
		}
		// Container Apps has no operation to read: the app is read until it
		// is gone.
		return deleteOutcome(ctx, args, goneWatch("ca_delete", name, map[string]any{"name": name}, op.ID, "ca_get",
			func(ctx context.Context) (map[string]any, error) { return cli.Get(ctx, name) }))
	}}, nil
}

func (s *Registry) handleCaLogs(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageT("name is required", "нужен name"), true
	}
	system, _ := args["system"].(bool)
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	res, err := cli.Logs(ctx, name, system, caLogsTail)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(caLogsOutput(res, system)), false
}

// caLogsTail is how many lines ca_logs answers: the newest by time.
const caLogsTail = 100

// caLogsDoc is the Evolution page on Container Apps logs.
const caLogsDoc = "https://cloud.ru/docs/container-apps-evolution/ug/topics/concepts__logging"

// caLogsOutput is the answer of ca_logs: the lines, and a note when there
// are none or the read was cut, so that an empty list does not read as
// "the app writes nothing" without a word.
func caLogsOutput(res *containerapp.LogsResult, system bool) map[string]any {
	out := map[string]any{"logs": res.Lines}
	switch {
	case res.Cut:
		out["truncated"] = true
		out["note"] = fmt.Sprintf("Read %d lines and the API still names a next page; it does not document the order of pages, so the newest lines may be missing. The console shows the full log: Container Apps → <app> → Logs (%s).", res.Read, caLogsDoc)
	case len(res.Lines) == 0 && system:
		out["note"] = "No system events for this container app. App logs (stdout/stderr): ca_logs without system."
	case len(res.Lines) == 0:
		out["note"] = fmt.Sprintf("No app log lines. App logs are what the container writes to stdout and stderr (%s); logs of the \"Request logging\" option go to the Logging service, not here. The delivery delay of logs is not documented: if the app has just served requests, call ca_logs again in a minute. system=true shows platform events (start, scaling, errors).", caLogsDoc)
	}
	return out
}

// planCaStartStop plans ca_start or ca_stop (action). The client is built
// in the execution, as it was after the question: a call without a key
// still asks first (moving that refusal before the question is a breaking
// change, the owner's to decide).
func (s *Registry) planCaStartStop(action string) Planner {
	return func(_ context.Context, args map[string]any) (*Plan, error) {
		name := stringArg(args, "name")
		if name == "" {
			return answered(usageT("name is required", "нужен name"), true), nil
		}
		msg := i18n.Tf("ca_start will start container app %q (it is billed while running).",
			"ca_start запустит приложение %q (пока оно работает, оно оплачивается).", name)
		if action == "stop" {
			msg = i18n.Tf("ca_stop will stop container app %q (it stops serving requests).",
				"ca_stop остановит приложение %q (оно перестанет обслуживать запросы).", name)
		}
		// The state of the app is read for the question only, and named
		// in it: a repeat — a start of an app that runs — asks as before
		// (its exit code does not change before the major) and shows what
		// the app is now. A state that cannot be read is said, not
		// refused: the call's own refusal is the platform's, as it was
		// (release plan 1.9, «Повтор у выпущенных инструментов», п. 4).
		preview := func(ctx context.Context) (map[string]any, error) {
			out := map[string]any{"message": msg}
			state, err := s.caState(ctx, name)
			switch {
			case err != nil && notThere(err):
				// Asked all the same: a refusal here would change the
				// exit code of a shipped command, which waits for the
				// major. The question says what the platform will answer.
				out["app_status_note"] = i18n.T("no app of this name was found: the call will be refused by the platform; ca_list shows the apps",
					"приложение с таким именем не найдено: платформа откажет вызову; приложения показывает ca_list")
			case err != nil:
				reason, _, _ := errReason(err, nil)
				out["app_status_note"] = i18n.Tf("the state of the app could not be read (%s): ca_get shows it", "состояние приложения прочитать не удалось (%s): его показывает ca_get", reason)
			case state != "":
				out["app_status"] = state
			}
			return out, nil
		}
		return &Plan{PreviewOf: preview, Execute: func(ctx context.Context) (string, bool) {
			cli, err := s.caClient(ctx)
			if err != nil {
				return errResult(err), true
			}
			op, err := cli.StartStop(ctx, name, action)
			if err != nil {
				return errResult(err), true
			}
			return caStartStopAnswer(ctx, cli, name, action, op)
		}}, nil
	}
}

// caStartStopAnswer is the answer of ca_start or ca_stop to the platform's
// operation: ok only when the operation is done (the answer as shipped);
// one that ended with an error is the platform's refusal; one still going
// on is starting or stopping, with the state of the app and its read as
// the next step — Container Apps has no tool that reads an operation, and
// the app is read until it runs or stops, as ca_delete reads it until it
// is gone (CLAUDE.md §6, axis 2; Астра #1813).
func caStartStopAnswer(ctx context.Context, cli *containerapp.Client, name, action string, op *containerapp.Operation) (string, bool) {
	if msg, code, failed := opFailure(op.Error); failed {
		e := clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("Cloud.ru did not %s container app %q: %s (operation %s)", "Cloud.ru не выполнил %s приложения %q: %s (операция %s)",
				action, name, quoteCloud(fmt.Sprint(msg)), op.ID),
			i18n.T("ca_get shows the state of the app.", "Состояние приложения показывает ca_get."))
		fields := map[string]any{"name": name, "action": action, "operation_id": op.ID}
		if code != nil {
			fields["platform_code"] = code
		}
		return errAnswer(e, fields), true
	}
	if op.Done {
		return toJSON(map[string]any{"status": "ok", "name": name, "action": action}), false
	}
	status := "starting"
	note := i18n.T("Cloud.ru took the command and has not finished it: the app starts now. ca_get shows its state.",
		"Cloud.ru принял команду и ещё не выполнил её: приложение запускается. Его состояние показывает ca_get.")
	if action == "stop" {
		status = "stopping"
		note = i18n.T("Cloud.ru took the command and has not finished it: the app stops now. ca_get shows its state.",
			"Cloud.ru принял команду и ещё не выполнил её: приложение останавливается. Его состояние показывает ca_get.")
	}
	// An answer with no operation (no id, not done) says nothing of the
	// command: the state of the app is not known («Риск», conclusion 227).
	if op.ID == "" {
		status = "waiting"
		note = i18n.T("Cloud.ru answered no operation: whether the command is carried out is not known. ca_get shows the state of the app.",
			"Cloud.ru не вернул операцию: выполняется ли команда, неизвестно. Состояние приложения показывает ca_get.")
	}
	// The specification does not list the states of an app: the note
	// names none.
	out := map[string]any{"status": status, "name": name, "action": action, "note": note}
	if m, err := cli.Get(ctx, name); err == nil {
		if st, _ := m["status"].(string); st != "" {
			out["app_status"] = st
		}
	}
	nextStep(out, "ca_get", map[string]any{"name": name})
	return toJSON(out), false
}
