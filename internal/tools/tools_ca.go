package tools

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/containerapp"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
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
		return usageResult(fmt.Errorf("name is required")), true
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
		return containerapp.CreateContainerReq{}, clierrors.New(clierrors.CodeUsage, "name and image are required", "")
	}
	if !caNameRe.MatchString(name) {
		return containerapp.CreateContainerReq{}, clierrors.New(clierrors.CodeUsage,
			fmt.Sprintf("name %q is not an RFC 1123 label", name),
			"Lowercase letters, digits and '-', 1-63 characters, starting and ending with a letter or digit, e.g. my-app.")
	}
	cpu := firstNonEmptyStr(stringArg(args, "cpu"), "0.1")
	memory := firstNonEmptyStr(stringArg(args, "memory"), "256Mi")
	port := intArg(args, "port", 8080)
	switch {
	case port < 1 || port > 65535:
		return containerapp.CreateContainerReq{}, clierrors.New(clierrors.CodeUsage,
			fmt.Sprintf("port %d is out of range 1-65535", port), "The default is 8080.")
	case port <= 1024 || slices.Contains(caReservedPorts, port):
		// Ports up to 1024 used to switch the container to privileged mode
		// (root) without a word in the preview; the platform reserves them.
		return containerapp.CreateContainerReq{}, clierrors.New(clierrors.CodeUsage,
			fmt.Sprintf("port %d is reserved by Container Apps", port),
			"Reserved: 1-1024, 8008, 8012, 8013, 8022, 8112, 9090, 9091, 9092. Pick another one, e.g. 8080 (the default); the app must listen on 0.0.0.0:$PORT.")
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
			fmt.Sprintf("min_instances %d and max_instances %d: need 0 <= min_instances <= max_instances and max_instances >= 1", minI, maxI),
			"The defaults are 0 (scale to zero) and 1.")
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

// handleCaDeploy creates+runs a container app (gated). Async.
func (s *Registry) handleCaDeploy(ctx context.Context, args map[string]any) (string, bool) {
	req, err := caDeployReq(args)
	if err != nil {
		return errResult(err), true
	}
	name := req.Name
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		msg := fmt.Sprintf("ca_deploy will create+run container app %q from image %q (billable). Call again with confirmed=true after the user approves.", name, req.Template.Containers[0].Image)
		if req.Configuration.Privileged {
			msg = fmt.Sprintf("ca_deploy will create+run container app %q from image %q in PRIVILEGED mode: the container runs as root (billable). Call again with confirmed=true after the user approves.", name, req.Template.Containers[0].Image)
		}
		return toJSON(map[string]any{
			"status":      "confirmation_required",
			"message":     msg,
			"will_create": caDeployPreview(req),
		}), true
	}

	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.Create(ctx, req)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"status":       "deploying",
		"name":         name,
		"operation_id": op.ID,
		"note":         "Container app is deploying (async). Use ca_get for status + public URL, ca_logs for logs. Scales to zero when idle.",
	}), false
}

func (s *Registry) handleCaDelete(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageResult(fmt.Errorf("name is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("ca_delete permanently deletes container app %q. Call again with confirmed=true after the user approves.", name),
		}), true
	}
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.Delete(ctx, name)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "deleting", "name": name, "operation_id": op.ID}), false
}

func (s *Registry) handleCaLogs(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageResult(fmt.Errorf("name is required")), true
	}
	system, _ := args["system"].(bool)
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	logs, err := cli.Logs(ctx, name, system)
	if err != nil {
		return errResult(err), true
	}
	if len(logs) > 100 {
		logs = logs[len(logs)-100:]
	}
	return toJSON(map[string]any{"logs": logs}), false
}

func (s *Registry) handleCaStartStop(ctx context.Context, args map[string]any, action string) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageResult(fmt.Errorf("name is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		effect := "it is billed while running"
		if action == "stop" {
			effect = "it stops serving requests"
		}
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("ca_%s will %s container app %q (%s). Call again with confirmed=true after the user approves.", action, action, name, effect),
		}), true
	}
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if err := cli.StartStop(ctx, name, action); err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "ok", "name": name, "action": action}), false
}

func (s *Registry) handleCaStart(ctx context.Context, args map[string]any) (string, bool) {
	return s.handleCaStartStop(ctx, args, "start")
}
func (s *Registry) handleCaStop(ctx context.Context, args map[string]any) (string, bool) {
	return s.handleCaStartStop(ctx, args, "stop")
}
