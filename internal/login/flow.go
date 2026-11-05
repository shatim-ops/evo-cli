// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/iamadmin"
)

// Audience is whom the key is for: the person (account evo) or an agent
// (account evo-agent).
type Audience string

// The two audiences of the consent.
const (
	ForMe    Audience = "me"
	ForAgent Audience = "agent"
)

// account is the service account of an audience.
func (a Audience) account() string {
	if a == ForAgent {
		return "evo-agent"
	}
	return "evo"
}

// Terms are the terms of a key the consent offers, in days; DefaultTerm is
// the one chosen unless the person picks another (contract B-1, section 1).
var Terms = []int{7, 30, 90}

// DefaultTerm is the term chosen by default.
const DefaultTerm = 30

// Choice is what the flags of evo login choose before the page: an empty
// field leaves it to the person.
type Choice struct {
	Project string
	For     Audience
	Days    int
}

// Previous is the key the profile holds now: replaced by the new one, it
// is deleted after the new one is kept (R-25: by its id, never by a name).
type Previous struct {
	ServiceAccountID, AccessKeyID string
}

// Saved is what the login keeps in the profile. Secret is the only secret.
type Saved struct {
	KeyID, Secret                              string
	ProjectID, CustomerID                      string
	ServiceAccountID, AccessKeyID, Role, Login string
	KeyExpiresAt, LoginAt                      time.Time
}

// Result is what the login answers: no secret, the key ID masked by the
// caller.
type Result struct {
	ProjectID, ProjectName, Account, Role, KeyID string
	AccountReused, PreviousRevoked               bool
	KeyExpiresAt                                 time.Time
	// PreviousNote says why the previous key is left, when it is.
	PreviousNote string
}

// project is a project the person may choose.
type project struct {
	ID, Name, Customer, CustomerName string
	Allowed                          bool
}

// session is one evo login: its secrets, what the person chose and what it
// made, for the rollback.
type session struct {
	cfg      Config
	choice   Choice
	previous *Previous
	save     func(Saved) error

	state, nonce, verifier, redirect string
	disc                             Discovery
	human                            string // the person's access token, in memory only
	who                              person
	projects                         []project
	admin                            *iamadmin.Client

	made []undo // what this login created, oldest first
}

// undo deletes one thing this login made.
type undo struct {
	what string
	do   func(context.Context) error
}

// humanToken is the person's access token as a token provider of
// iamadmin.
type humanToken string

func (t humanToken) Token(context.Context) (auth.Token, error) {
	return auth.Token{Value: string(t), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// exchange trades the code for the person's tokens and checks the ID
// token (steps 6–7).
func (s *session) exchange(ctx context.Context, code string) error {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {s.redirect},
		"client_id": {auth.LoginClientID}, "code_verifier": {s.verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.cfg.HTTP.Do(req)
	if err != nil {
		return clierrors.Wrap(clierrors.CodeAPIUnreachable,
			i18n.T("the OpenID Connect provider of Cloud.ru is unreachable", "провайдер OpenID Connect Cloud.ru недоступен"),
			clierrors.NetworkHint(err, i18n.T("Check the network and run 'evo login' again.", "Проверьте сеть и повторите 'evo login'.")), err)
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" || tok.IDToken == "" {
		return clierrors.New(clierrors.CodeAuthFailed,
			i18n.Tf("the sign-in session expired or its code was not taken (HTTP %d); nothing was created",
				"сессия входа истекла или её код не принят (HTTP %d); ничего не создано", resp.StatusCode),
			i18n.T("Run 'evo login' again.", "Повторите 'evo login'."))
	}
	who, err := s.cfg.verifyIDToken(ctx, s.disc.JWKSURI, tok.IDToken, s.cfg.Issuer, s.nonce, s.cfg.Now())
	if err != nil {
		return err
	}
	s.human, s.who = tok.AccessToken, who
	s.admin = iamadmin.New(humanToken(s.human), s.cfg.AdminBase)
	return nil
}

// prepare reads the organizations and projects of the person and whether
// each lets them make the account and grant the role (steps 8–10).
func (s *session) prepare(ctx context.Context) error {
	customers, err := s.orgList(ctx, "/v3/customers", nil, "customers")
	if err != nil {
		return err
	}
	for _, c := range customers {
		ps, err := s.orgList(ctx, "/v3/projects", url.Values{"customer_ids": {str(c, "id")}}, "projects")
		if err != nil {
			return err
		}
		for _, p := range ps {
			if str(p, "status") == "deleted" {
				continue
			}
			s.projects = append(s.projects, project{ID: str(p, "id"), Name: str(p, "name"),
				Customer: str(c, "id"), CustomerName: str(c, "name")})
		}
	}
	if s.choice.Project != "" {
		s.projects = slices.DeleteFunc(s.projects, func(p project) bool {
			return p.ID != s.choice.Project && p.Name != s.choice.Project
		})
		if len(s.projects) == 0 {
			return clierrors.New(clierrors.CodeUsage,
				i18n.Tf("no project %q among the projects of %s; nothing was created", "проекта %q нет среди проектов %s; ничего не создано",
					s.choice.Project, s.who.Login),
				i18n.T("Pass a project's id or name the person sees in the console, or no --project to choose on the page.",
					"Передайте id или имя проекта, который человек видит в консоли, или не передавайте --project и выберите на странице."))
		}
	}
	if len(s.projects) == 0 {
		return clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%s sees no project; nothing was created", "%s не видит ни одного проекта; ничего не создано", s.who.Login),
			i18n.T("A project is created in the console; or sign in as a member of one.", "Проект создаётся в консоли; или войдите участником проекта."))
	}
	var checks []iamadmin.Check
	for _, p := range s.projects {
		checks = append(checks, iamadmin.Check{Action: auth.LoginActionCreateAccount, ResourceID: p.ID},
			iamadmin.Check{Action: auth.LoginActionGrantRole, ResourceID: p.ID})
	}
	got, err := s.admin.BulkEvaluate(ctx, checks)
	if err != nil {
		return err
	}
	anyAllowed := false
	for i := range s.projects {
		s.projects[i].Allowed = got[2*i] && got[2*i+1]
		anyAllowed = anyAllowed || s.projects[i].Allowed
	}
	if !anyAllowed {
		return noRight(s.projects[0].Name)
	}
	return nil
}

// noRight is the refusal of a person who may not make the access: AUTH_FAILED
// before any write (contract B-1, step 12).
func noRight(projectName string) error {
	return clierrors.New(clierrors.CodeAuthFailed,
		i18n.Tf("creating access to the project %q takes its administrator; nothing was created",
			"создать доступ к проекту %q может администратор проекта; ничего не создано", projectName),
		i18n.T("Ask the project administrator to run 'evo login', or to create a key in the console and pass it to you ('evo configure').",
			"Попросите администратора проекта выполнить 'evo login' или создать ключ в консоли и передать вам ('evo configure')."))
}

// orgList reads every page of a list of the Organization API with the
// person's token.
func (s *session) orgList(ctx context.Context, path string, q url.Values, key string) ([]map[string]any, error) {
	var out []map[string]any
	token := ""
	for page := 0; page < iamadmin.MaxPages; page++ {
		pq := url.Values{}
		for k, v := range q {
			pq[k] = v
		}
		if token != "" {
			pq.Set("page_token", token)
		}
		u := strings.TrimRight(s.cfg.OrgBase, "/") + path
		if len(pq) > 0 {
			u += "?" + pq.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+s.human)
		resp, err := s.cfg.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("organization request failed: %w", "запрос к Organization API не удался: %w"), err)
		}
		var m map[string]any
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &m) != nil {
			return nil, clierrors.New(clierrors.CodeAPIError,
				i18n.Tf("Organization API %d at %s", "Organization API %d на %s", resp.StatusCode, path), "")
		}
		xs, _ := m[key].([]any)
		for _, x := range xs {
			if o, ok := x.(map[string]any); ok {
				out = append(out, o)
			}
		}
		if token, _ = m["next_page_token"].(string); token == "" {
			return out, nil
		}
	}
	return nil, clierrors.New(clierrors.CodeAPIError, i18n.Tf("Organization API: %s has too many pages", "Organization API: у %s слишком много страниц", path), "")
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// grant makes the access the person chose (steps 12–18), the key last; a
// failure after a write undoes what this login made, newest first (R-44).
func (s *session) grant(ctx context.Context, p project, aud Audience, days int) (Result, error) {
	res, err := s.make(ctx, p, aud, days)
	if err != nil && len(s.made) > 0 {
		return res, s.rollback(ctx, err)
	}
	return res, err
}

func (s *session) make(ctx context.Context, p project, aud Audience, days int) (Result, error) {
	res := Result{ProjectID: p.ID, ProjectName: p.Name, Account: aud.account(), Role: auth.LoginRoleName}
	// 12. Asked again for the chosen project, before any write.
	for _, a := range []string{auth.LoginActionCreateAccount, auth.LoginActionGrantRole} {
		ok, err := s.admin.Evaluate(ctx, iamadmin.Check{Action: a, ResourceID: p.ID})
		if err != nil {
			return res, err
		}
		if !ok {
			return res, noRight(p.Name)
		}
	}
	now := s.cfg.Now()
	term := now.Add(time.Duration(days) * 24 * time.Hour).UTC().Truncate(time.Second)
	desc := fmt.Sprintf("evo login, %s, %s", s.who.Login, now.UTC().Format("2006-01-02"))

	// 13. The account by its name: reused, its keys within the limit.
	sas, err := s.admin.ServiceAccounts(ctx, p.ID, aud.account())
	if err != nil {
		return res, err
	}
	sa := ""
	if len(sas) > 0 {
		sa, res.AccountReused = sas[0].ID, true
		keys, err := s.admin.AccessKeys(ctx, sa)
		if err != nil {
			return res, err
		}
		if len(keys) >= maxKeys {
			return res, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("the account %s has %d access keys, the limit; nothing was created", "у аккаунта %s %d ключей доступа — это предел; ничего не создано", aud.account(), len(keys)),
				i18n.T("Revoke the keys no one uses in the console (Users → Service accounts) and run 'evo login' again.",
					"Отзовите неиспользуемые ключи в консоли («Пользователи» → «Сервисные аккаунты») и повторите 'evo login'."))
		}
	} else {
		all, err := s.admin.ServiceAccounts(ctx, p.ID, "")
		if err != nil {
			return res, err
		}
		if len(all) >= maxAccounts {
			return res, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("the project %s has %d service accounts, the limit; nothing was created", "в проекте %s уже %d сервисных аккаунтов — это предел; ничего не создано", p.Name, len(all)),
				i18n.T("Delete an account no one uses in the console (Users → Service accounts) and run 'evo login' again.",
					"Удалите неиспользуемый аккаунт в консоли («Пользователи» → «Сервисные аккаунты») и повторите 'evo login'."))
		}
		// 14. Create it; a 409 is another login's: list again and reuse.
		a, err := s.admin.CreateServiceAccount(ctx, p.ID, aud.account(), desc)
		switch {
		case err == nil:
			sa = a.ID
			s.made = append(s.made, undo{i18n.Tf("service account %s", "сервисный аккаунт %s", aud.account()),
				func(ctx context.Context) error { return s.admin.DeleteServiceAccount(ctx, a.ID) }})
		case iamadmin.StatusOf(err) == http.StatusConflict:
			again, lerr := s.admin.ServiceAccounts(ctx, p.ID, aud.account())
			if lerr != nil || len(again) == 0 {
				return res, err
			}
			sa, res.AccountReused = again[0].ID, true
		default:
			return res, err
		}
	}

	// 15. The role until the key's term: a shorter one is moved forward,
	// a longer one kept, none created.
	perms, err := s.admin.RolePermissions(ctx, p.ID, sa, auth.LoginRoleName)
	if err != nil {
		return res, err
	}
	if len(perms) > 0 {
		have, perr := time.Parse(time.RFC3339, perms[0].ExpiresAt)
		if perms[0].ExpiresAt != "" && (perr != nil || have.Before(term)) {
			if err := s.admin.ExtendPermission(ctx, perms[0].ID, term); err != nil {
				return res, err
			}
		}
	} else {
		g := iamadmin.Grant{SubjectID: sa, ResourceID: p.ID, RoleName: auth.LoginRoleName, ExpiresAt: term}
		perm, err := s.admin.CreatePermission(ctx, g)
		if iamadmin.StatusOf(err) == http.StatusBadRequest {
			// A custom role of the same name: the product role by its id.
			id, rerr := s.admin.RoleID(ctx, p.Customer, auth.LoginRoleName)
			if rerr != nil {
				return res, err
			}
			g.RoleName, g.RoleID = "", id
			perm, err = s.admin.CreatePermission(ctx, g)
		}
		if err != nil {
			return res, err
		}
		s.made = append(s.made, undo{i18n.T("the role of the account", "роль аккаунта"),
			func(ctx context.Context) error { return s.admin.DeletePermission(ctx, perm.ID) }})
	}

	// 16. The key, last.
	key, err := s.admin.CreateAccessKey(ctx, sa, desc, time.Duration(days)*24*time.Hour)
	if err != nil {
		return res, err
	}
	s.made = append(s.made, undo{i18n.T("the access key", "ключ доступа"),
		func(ctx context.Context) error { return s.admin.DeleteAccessKey(ctx, sa, key.ID) }})
	res.KeyID = key.KeyID
	res.KeyExpiresAt = term
	if t, err := time.Parse(time.RFC3339, key.ExpiredAt); err == nil {
		res.KeyExpiresAt = t
	}

	// 17. The key works, by the exchange of every later command.
	if err := s.checkKey(ctx, key); err != nil {
		return res, err
	}

	// 18. The profile, at once (R-44); then the previous key goes.
	if err := s.save(Saved{KeyID: key.KeyID, Secret: key.Secret, ProjectID: p.ID, CustomerID: p.Customer,
		ServiceAccountID: sa, AccessKeyID: key.ID, Role: auth.LoginRoleName, Login: s.who.Login,
		KeyExpiresAt: res.KeyExpiresAt, LoginAt: now}); err != nil {
		return res, err
	}
	s.made = nil // kept: nothing to undo now
	if prev := s.previous; prev != nil && prev.AccessKeyID != "" && prev.AccessKeyID != key.ID {
		if err := s.admin.DeleteAccessKey(ctx, prev.ServiceAccountID, prev.AccessKeyID); err != nil && iamadmin.StatusOf(err) != http.StatusNotFound {
			res.PreviousNote = i18n.Tf("the previous key %s is left: %v — revoke it in the console", "прежний ключ %s остался: %v — отзовите его в консоли",
				prev.AccessKeyID, err)
		} else {
			res.PreviousRevoked = true
		}
	}
	return res, nil
}

// The limits of IAM, refused before any write (#563; to be confirmed by
// IAM): service accounts of a project, access keys of an account.
const (
	maxAccounts = 10
	maxKeys     = 50
)

// checkKey exchanges the new key for a token as every later command will.
func (s *session) checkKey(ctx context.Context, key iamadmin.NewKey) error {
	body, _ := json.Marshal(map[string]string{"keyId": key.KeyID, "secret": key.Secret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.KeyToken, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.cfg.HTTP.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	if err != nil || resp.StatusCode != http.StatusOK {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return clierrors.New(clierrors.CodeAuthFailed,
			i18n.Tf("the new key did not get an IAM token (HTTP %d)", "новый ключ не получил токен IAM (HTTP %d)", status),
			i18n.T("Run 'evo login' again; if it repeats, report it.", "Повторите 'evo login'; если повторится — сообщите об этом."))
	}
	return nil
}

// rollback deletes what this login made, newest first, with the person's
// token. What it could not delete is named, with the way: the console.
func (s *session) rollback(ctx context.Context, cause error) error {
	// The rollback runs even when the login's context is done.
	ctx = context.WithoutCancel(ctx)
	var undone, left []string
	for i := len(s.made) - 1; i >= 0; i-- {
		if err := s.made[i].do(ctx); err != nil && iamadmin.StatusOf(err) != http.StatusNotFound {
			left = append(left, s.made[i].what)
		} else {
			undone = append(undone, s.made[i].what)
		}
	}
	s.made = nil
	if len(left) == 0 {
		code := clierrors.Reason(cause)
		if !clierrors.IsCode(code) {
			code = clierrors.CodeAPIError
		}
		return clierrors.Wrap(code,
			i18n.Tf("access is not complete: %v; undone: %s, nothing is left", "доступ не завершён: %v; отменено: %s, ничего не осталось",
				cause, strings.Join(undone, ", ")),
			i18n.T("Run 'evo login' again.", "Повторите 'evo login'."), cause)
	}
	return clierrors.Wrap(clierrors.CodeAPIError,
		i18n.Tf("access is not complete: %v; undone: %s; left: %s", "доступ не завершён: %v; отменено: %s; осталось: %s",
			cause, strings.Join(undone, ", "), strings.Join(left, ", ")),
		i18n.T("Delete what is left in the console (Users → Service accounts), then run 'evo login' again.",
			"Удалите оставшееся в консоли («Пользователи» → «Сервисные аккаунты») и повторите 'evo login'."), cause)
}
