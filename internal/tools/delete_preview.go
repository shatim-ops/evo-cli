package tools

import (
	"fmt"
	"strings"
)

// deletePreview is the confirmation answer of a *_delete tool: one GET of
// the resource names what goes — its name and status next to the ID the
// caller passed — and an ID that is not there is that GET's error instead
// of a question (release audit, R-04). message has one %s: the resource
// as "name (id)".
func deletePreview(message, id string, get func() (map[string]any, error)) (string, bool) {
	m, err := get()
	if err != nil {
		return errResult(err), true
	}
	name, _ := m["name"].(string)
	status, _ := m["status"].(string)
	if status == "" {
		status, _ = m["state"].(string)
	}
	label := id
	if name != "" {
		label = fmt.Sprintf("%s (%s)", name, id)
	}
	out := map[string]any{
		"status":  "confirmation_required",
		"message": fmt.Sprintf(message, label),
		"target":  map[string]any{"id": id, "name": name, "status": status},
	}
	// Cloud.ru refuses to delete a DBaaS cluster that is still being
	// created (400/500 FailedPrecondition, register DB-17, CRD-B15): said
	// before the confirmation, not after (release audit re-run, N-09).
	switch strings.ToUpper(status) {
	case "DEPLOYING", "CREATING", "PROVISIONING":
		out["warning"] = fmt.Sprintf("it is still being created (%s): Cloud.ru refuses the delete until it is ready", status)
	}
	return toJSON(out), true
}
