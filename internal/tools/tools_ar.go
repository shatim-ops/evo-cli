package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/artifact"
)

// arClient builds an authenticated Artifact Registry client.
func (s *Registry) arClient(ctx context.Context) (*artifact.Client, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return artifact.New(tok.Value, eng.Config.ProjectID, ""), nil
}

func (s *Registry) handleArList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.arClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListRegistries(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id": m["id"], "name": m["name"], "type": m["registryType"],
			"status": m["status"], "public": m["isPublic"], "tariff": m["tariff"],
		})
	}
	return toJSON(map[string]any{"registries": out}), false
}

func (s *Registry) handleArGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageResult(fmt.Errorf("'id' is required (from ar_list)")), true
	}
	cli, err := s.arClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetRegistry(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

func (s *Registry) handleArCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageResult(fmt.Errorf("'name' is required")), true
	}
	regType := strings.ToUpper(stringArg(args, "registry_type"))
	if regType == "" {
		regType = "DOCKER"
	}
	public, _ := args["public"].(bool)
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("ar_create makes a BILLABLE %s registry %q (public=%v). Call again with confirmed=true after the user approves.", regType, name, public),
		}), true
	}
	cli, err := s.arClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	// A taken name made a second registry, rc 0 (release audit, L-18).
	existing, err := cli.ListRegistries(ctx)
	if err != nil {
		return errResult(fmt.Errorf("check the name is free: list registries: %w", err)), true
	}
	if id, st, ok := takenName(existing, name); ok {
		return nameTakenResult("ar_create", "registry", name, id, st, "ar_list")
	}
	body := map[string]any{
		"projectId":    cli.ProjectID(),
		"name":         name,
		"registryType": regType,
		"isPublic":     public,
	}
	if t := strings.ToUpper(stringArg(args, "tariff")); t != "" {
		body["tariff"] = t
	}
	op, err := cli.CreateRegistry(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create registry: %w", err)), true
	}
	return toJSON(map[string]any{
		"status": "creating", "operation_id": op.ID,
		"note": "Async — poll ar_operation(operation_id) until done=true; then ar_list for the id/status ACTIVE. Push images to <registry>.cr.cloud.ru (docker login with the SA key).",
	}), false
}

func (s *Registry) handleArDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageResult(fmt.Errorf("'id' is required (from ar_list)")), true
	}
	cli, err := s.arClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return deletePreview("ar_delete PERMANENTLY deletes registry %s and its artifacts. Call again with confirmed=true.",
			id, func() (map[string]any, error) { return cli.GetRegistry(ctx, id) })
	}
	op, err := cli.DeleteRegistry(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete registry: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}

func (s *Registry) handleArOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageResult(fmt.Errorf("'operation_id' is required")), true
	}
	cli, err := s.arClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if w := pgInt(args, "wait_seconds", 0); w > 0 {
		m, err := waitForOpJSON(ctx, w, func(ctx context.Context) (map[string]any, error) {
			op, err := cli.GetOperation(ctx, id)
			if err != nil {
				return nil, err
			}
			return opToMap(op), nil
		})
		if err != nil {
			return errResult(err), true
		}
		return toJSON(m), false
	}
	op, err := cli.GetOperation(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(op), false
}
