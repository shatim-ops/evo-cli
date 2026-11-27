// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package iamadmin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

// The requests of evo login (B-3, contract B-1, steps 10–18 and the
// rollback), with the token of the person who signed in; logout deletes
// the key with the token of its own account.

// Check is one question of evaluate: may the caller do Action on
// ResourceID. A person asks for themselves: userId and serviceAccountId
// are only for a service account that asks (the spec), never sent here.
type Check struct {
	Action, ResourceID string
}

// BulkEvaluate answers checks in one request, in their order; each result
// is matched by the request it echoes when the answer echoes them, else by
// its place. A check without a result is an error, never «denied».
func (c *Client) BulkEvaluate(ctx context.Context, checks []Check) ([]bool, error) {
	reqs := make([]map[string]string, len(checks))
	for i, ch := range checks {
		reqs[i] = map[string]string{"action": ch.Action, "resourceId": ch.ResourceID}
	}
	var m map[string]any
	if err := c.send(ctx, http.MethodPost, "/api/v1/permissions/bulk-evaluate", nil, map[string]any{"requests": reqs}, &m); err != nil {
		return nil, err
	}
	results := items(m, "results")
	if len(results) != len(checks) {
		return nil, fmt.Errorf("iam: bulk-evaluate answered %d results to %d checks", len(results), len(checks))
	}
	byEcho := map[Check]bool{}
	for _, r := range results {
		if e, ok := field(r, "request").(map[string]any); ok {
			byEcho[Check{Action: str(e, "action"), ResourceID: str(e, "resourceId")}] = flag(r, "accessGranted")
		}
	}
	out := make([]bool, len(checks))
	for i, ch := range checks {
		if v, ok := byEcho[ch]; ok {
			out[i] = v
		} else {
			out[i] = flag(results[i], "accessGranted")
		}
	}
	return out, nil
}

// Actions lists the names of the IAM catalog of actions. evo login reads
// it only when evaluate answers false, to tell «no right» from «no such
// action» («Риск», R-49).
func (c *Client) Actions(ctx context.Context) ([]string, error) {
	return list(ctx, c, "actions", "/api/v1/actions", nil, "actions", "name", func(m map[string]any) (string, error) {
		return str(m, "name"), stringFields(m, []string{"name"})
	})
}

// Evaluate answers one check.
func (c *Client) Evaluate(ctx context.Context, ch Check) (bool, error) {
	var m map[string]any
	q := url.Values{"action": {ch.Action}, "resourceId": {ch.ResourceID}}
	if err := c.do(ctx, "/api/v1/permissions/evaluate", q, &m); err != nil {
		return false, err
	}
	return flag(m, "accessGranted"), nil
}

// ServiceAccount is a service account of a project.
type ServiceAccount struct {
	ID, Name string
}

// ServiceAccounts lists the service accounts of a project, those named
// name only when it is not "".
func (c *Client) ServiceAccounts(ctx context.Context, projectID, name string) ([]ServiceAccount, error) {
	if !pathID.MatchString(projectID) {
		return nil, badValue("target.projectId", projectID)
	}
	q := url.Values{"target.projectId": {projectID}}
	if err := setFilter(q, "name", name); err != nil {
		return nil, err
	}
	required := []string{"id"}
	if name != "" {
		required = append(required, "name") // used to prove whether login should create an account
	}
	return list(ctx, c, "service accounts", "/api/v2/serviceAccounts", q, "serviceAccounts", "id", func(m map[string]any) (ServiceAccount, error) {
		if err := stringFields(m, required, "name"); err != nil {
			return ServiceAccount{}, err
		}
		sa := ServiceAccount{ID: str(m, "id"), Name: str(m, "name")}
		if name != "" && sa.Name != name {
			return ServiceAccount{}, pager.Incomplete(i18n.T("IAM returned an account outside the name filter", "IAM вернул аккаунт с другим именем"))
		}
		return sa, nil
	})
}

// CreateServiceAccount creates the service account name in a project.
func (c *Client) CreateServiceAccount(ctx context.Context, projectID, name, description string) (ServiceAccount, error) {
	var m map[string]any
	body := map[string]any{"name": name, "description": description, "target": map[string]string{"projectId": projectID}}
	if err := c.send(ctx, http.MethodPost, "/api/v2/serviceAccounts", nil, body, &m); err != nil {
		return ServiceAccount{}, err
	}
	return ServiceAccount{ID: str(m, "id"), Name: str(m, "name")}, nil
}

// DeleteServiceAccount deletes a service account (the rollback of a login
// that created it).
func (c *Client) DeleteServiceAccount(ctx context.Context, id string) error {
	if !pathID.MatchString(id) {
		return badValue("serviceAccountId", id)
	}
	return c.send(ctx, http.MethodDelete, "/api/v2/serviceAccounts/"+id, nil, nil, nil)
}

// RolePermissions lists the permissions of subjectID on resourceID with the
// role roleName.
func (c *Client) RolePermissions(ctx context.Context, resourceID, subjectID, roleName string) ([]Permission, error) {
	q := url.Values{}
	if err := setFilter(q, "resource_id", resourceID, "subject_id", subjectID, "role_name", roleName); err != nil {
		return nil, err
	}
	return list(ctx, c, "permissions", "/api/v3/permissions", q, "permissions", "id", func(row map[string]any) (Permission, error) {
		return readPermission(row, true)
	})
}

// Grant is a permission to create: the role by its name, or by its id
// when the name collides with a custom role's (the spec).
type Grant struct {
	SubjectID, ResourceID, RoleName, RoleID string
	ExpiresAt                               time.Time
}

// CreatePermission creates a permission.
func (c *Client) CreatePermission(ctx context.Context, g Grant) (Permission, error) {
	role := map[string]string{"roleName": g.RoleName}
	if g.RoleID != "" {
		role = map[string]string{"roleId": g.RoleID}
	}
	body := map[string]any{"subjectId": g.SubjectID, "resourceId": g.ResourceID, "roleIdentifier": role,
		"expiresAt": g.ExpiresAt.UTC().Format(time.RFC3339)}
	var m map[string]any
	if err := c.send(ctx, http.MethodPost, "/api/v3/permissions", nil, body, &m); err != nil {
		return Permission{}, err
	}
	return permissionOf(m), nil
}

// ExtendPermission moves the term of a permission to expiresAt.
func (c *Client) ExtendPermission(ctx context.Context, id string, expiresAt time.Time) error {
	if !pathID.MatchString(id) {
		return badValue("permissionId", id)
	}
	return c.send(ctx, http.MethodPut, "/api/v3/permissions/"+id, nil,
		map[string]string{"expiresAt": expiresAt.UTC().Format(time.RFC3339)}, nil)
}

// DeletePermission deletes a permission (the rollback of a login that
// created it).
func (c *Client) DeletePermission(ctx context.Context, id string) error {
	if !pathID.MatchString(id) {
		return badValue("permissionId", id)
	}
	return c.send(ctx, http.MethodDelete, "/api/v3/permissions/"+id, nil, nil, nil)
}

// RoleID is the id of the product role name in an organization: the way
// around a collision of names.
func (c *Client) RoleID(ctx context.Context, customerID, name string) (string, error) {
	q := url.Values{"customerId": {customerID}}
	if err := setFilter(q, "type", "ROLE_TYPE_PRODUCT", "name", name); err != nil {
		return "", err
	}
	ids, err := list(ctx, c, "roles", "/api/v1/roles", q, "roles", "id", func(m map[string]any) (string, error) {
		if err := stringFields(m, []string{"id", "name"}); err != nil {
			return "", err
		}
		if str(m, "name") == name {
			return str(m, "id"), nil
		}
		return "", nil
	})
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("iam: no product role %s", name)
}

// NewKey is an access key just created: its secret is in this answer only.
type NewKey struct {
	ID, KeyID, Secret, ExpiredAt string
}

// CreateAccessKey creates an access key of a service account for ttl.
func (c *Client) CreateAccessKey(ctx context.Context, serviceAccountID, description string, ttl time.Duration) (NewKey, error) {
	if !pathID.MatchString(serviceAccountID) {
		return NewKey{}, badValue("serviceAccountId", serviceAccountID)
	}
	var m map[string]any
	body := map[string]string{"description": description, "ttl": fmt.Sprintf("%dh", int(ttl.Hours()))}
	if err := c.send(ctx, http.MethodPost, "/api/v2/serviceAccounts/"+serviceAccountID+"/accessKeys", nil, body, &m); err != nil {
		return NewKey{}, err
	}
	return NewKey{ID: str(m, "id"), KeyID: str(m, "keyId"), Secret: str(m, "secret"), ExpiredAt: str(m, "expiredAt")}, nil
}

// DeleteAccessKey deletes the access key id of a service account.
func (c *Client) DeleteAccessKey(ctx context.Context, serviceAccountID, id string) error {
	if !pathID.MatchString(serviceAccountID) || !pathID.MatchString(id) {
		return badValue("accessKeyId", serviceAccountID+"/"+id)
	}
	return c.send(ctx, http.MethodDelete, "/api/v2/serviceAccounts/"+serviceAccountID+"/accessKeys/"+id, nil, nil, nil)
}

func permissionOf(m map[string]any) Permission {
	return Permission{ID: str(m, "id"), RoleName: str(m, "roleName"), RoleID: str(m, "primaryRoleId"),
		ResourceID: str(m, "resourceId"), SubjectID: str(m, "subjectId"), SubjectType: str(m, "subjectType"),
		ExpiresAt: str(m, "expiresAt"), Enabled: flag(m, "enabled")}
}
