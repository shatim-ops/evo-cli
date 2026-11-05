// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
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

// Choice is what the flags of evo login, or the terminal, choose before the
// page: the page offers it chosen; an empty field leaves it to the page.
type Choice struct {
	Project string
	For     Audience
	Days    int
}

// Store is where the key goes: the profile of an audience, the key it holds
// now and the keeping of the new one. The audience is known only after the
// choice, so the profile is too.
type Store interface {
	Profile(Audience) string
	Previous(Audience) *Previous
	Save(Audience, Saved) error
}

// Previous is the key the profile holds now: replaced by the new one, it
// is deleted after the new one is kept (R-25: by its id, never by a name).
type Previous struct {
	ServiceAccountID, AccessKeyID, KeyID string
}

// Saved is what the login keeps in the profile. Secret is the only secret.
type Saved struct {
	KeyID, Secret                              string
	ProjectID, CustomerID                      string
	ServiceAccountID, AccessKeyID, Role, Login string
	KeyExpiresAt, LoginAt                      time.Time
}

// Result is what the login answers: no secret, the key IDs masked by the
// caller.
type Result struct {
	Profile, ProjectID, ProjectName, Account, Role, KeyID string
	AccountReused                                         bool
	// RevokedKeyID is the key ID of the previous key, revoked; PreviousNote
	// says why it is left, when it is.
	RevokedKeyID, PreviousNote string
	KeyExpiresAt               time.Time
}

// The steps a refusal names in details "step", with "http_status" when an
// answer of Cloud.ru made it (the owner's decision, #981: the live runs of
// B-5 are read by them). Never a token or a code.
const (
	stepDiscovery  = "discovery"
	stepListen     = "listen"
	stepWait       = "wait"
	stepCallback   = "callback"
	stepToken      = "token"
	stepIDToken    = "id_token"
	stepCustomers  = "customers"
	stepProjects   = "projects"
	stepBulk       = "bulk-evaluate"
	stepChoice     = "choice"
	stepEvaluate   = "evaluate"
	stepAccount    = "account"
	stepPermission = "permission"
	stepKey        = "key"
	stepKeyCheck   = "key-check"
	stepProfile    = "profile"
)

// at names the step of a refusal: details step and http_status, and the
// same in its text. An error without a code becomes API_ERROR; an
// interruption stays one. A step already named is kept: the deepest wins.
func at(step string, err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) {
		code := clierrors.Reason(err)
		if !clierrors.IsCode(code) {
			code = clierrors.CodeAPIError
		}
		ce = clierrors.Wrap(code, err.Error(), eRetry(), err)
		err = ce
	}
	if _, named := ce.Details["step"]; named {
		return err
	}
	status := iamadmin.StatusOf(err)
	if s, ok := ce.Details["http_status"].(int); ok {
		status = s
	}
	details := map[string]any{"step": step}
	for k, v := range ce.Details {
		details[k] = v
	}
	if status != 0 {
		details["http_status"] = status
	}
	ce.Details = details
	ce.Message += tAt(step, status)
	return err
}

// withStatus is a refusal made of an HTTP answer of Cloud.ru, its status
// in the details for at.
func withStatus(ce *clierrors.CLIError, status int) *clierrors.CLIError {
	if status != 0 {
		ce.Details = map[string]any{"http_status": status}
	}
	return ce
}

// project is a project the person may choose.
type project struct {
	ID, Name, Customer, CustomerName string
	Allowed                          bool
}

// session is one evo login: its secrets, what the person chose and what it
// made, for the rollback.
type session struct {
	cfg    Config
	choice Choice
	store  Store

	// consent is the token of the consent page (R-47): made with the page,
	// never printed; /grant and /cancel take it with the state.
	state, nonce, verifier, redirect, consent string
	disc                                      Discovery
	human                                     string // the person's access token, in memory only
	who                                       person
	projects                                  []project
	unchecked                                 string // why evaluate gave no answer (U-9)
	admin                                     *iamadmin.Client

	made []undo // what this login created, oldest first

	tellMu sync.Mutex
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
		return at(stepToken, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.cfg.HTTP.Do(req)
	if err != nil {
		return at(stepToken, clierrors.Wrap(clierrors.CodeAPIUnreachable,
			eOIDCUnreachable(),
			clierrors.NetworkHint(err, eRetry()), err))
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" || tok.IDToken == "" {
		return at(stepToken, withStatus(clierrors.New(clierrors.CodeAuthFailed, eSession(resp.StatusCode), eRetry()), resp.StatusCode))
	}
	who, err := s.cfg.verifyIDToken(ctx, s.disc.JWKSURI, tok.IDToken, s.cfg.Issuer, s.nonce, s.cfg.Now())
	if err != nil {
		return at(stepIDToken, err)
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
		return at(stepCustomers, err)
	}
	for _, c := range customers {
		ps, err := s.orgList(ctx, "/v3/projects", url.Values{"customer_ids": {str(c, "id")}}, "projects")
		if err != nil {
			return at(stepProjects, err)
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
	}
	if len(s.projects) == 0 {
		hint := eNoProjectHint()
		if s.choice.Project != "" {
			hint = eNoProjectHintFlag()
		}
		return at(stepProjects, clierrors.New(clierrors.CodeUsage, eNoProject(s.choice.Project, s.who.Login), hint))
	}
	var checks []iamadmin.Check
	for _, p := range s.projects {
		checks = append(checks, iamadmin.Check{Action: auth.LoginActionCreateAccount, ResourceID: p.ID},
			iamadmin.Check{Action: auth.LoginActionGrantRole, ResourceID: p.ID})
	}
	got, err := s.admin.BulkEvaluate(ctx, checks)
	if err != nil {
		// U-9: only an explicit «denied» refuses before a write; the
		// first write gives the platform's answer otherwise.
		if err := s.uncheck(err); err != nil {
			return at(stepBulk, err)
		}
		got = make([]bool, len(checks))
		for i := range got {
			got[i] = true
		}
	}
	anyAllowed := false
	for i := range s.projects {
		s.projects[i].Allowed = got[2*i] && got[2*i+1]
		anyAllowed = anyAllowed || s.projects[i].Allowed
	}
	if !anyAllowed {
		return at(stepBulk, noRightIn(s.projects))
	}
	return nil
}

// uncheck takes an error of evaluate as no answer, not a refusal («Риск»,
// U-9): the names of the actions are a hypothesis until the probe, and a
// wrong one must not refuse everyone. The terminal and the page say the
// right is not checked. An interruption stays one.
func (s *session) uncheck(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if s.unchecked == "" {
		s.unchecked = oneLine(err.Error())
		s.tell(tUnchecked(s.unchecked))
	}
	return nil
}

// noRightIn is the refusal of a person with the right in none of their
// projects: the one project by its name, or how many there are.
func noRightIn(ps []project) error {
	if len(ps) == 1 {
		return noRight(ps[0].Name)
	}
	return clierrors.New(clierrors.CodeAuthFailed, eNoRightAny(len(ps)), eNoRightHint())
}

// noRight is the refusal of a person who may not make the access: AUTH_FAILED
// before any write (contract B-1, step 12).
func noRight(projectName string) error {
	return clierrors.New(clierrors.CodeAuthFailed, eNoRight(projectName), eNoRightHint())
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
			return nil, clierrors.Wrap(clierrors.CodeAPIUnreachable,
				eOrgUnreachable(),
				clierrors.NetworkHint(err, eRetry()), err)
		}
		var m map[string]any
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &m) != nil {
			return nil, withStatus(clierrors.New(clierrors.CodeAPIError,
				eOrgStatus(resp.StatusCode, path), eRetry()), resp.StatusCode)
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
	return nil, clierrors.New(clierrors.CodeAPIError, eOrgPages(path), "")
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// plan is the access the person chose, with what is known of it before
// any write: the account there and its keys, the profile and its key.
type plan struct {
	p        project
	aud      Audience
	days     int
	term     time.Time
	saID     string // the account of that name, when there is one
	keys     int    // its keys
	profile  string
	previous *Previous
}

// planFor checks the chosen access before the question (axis 1 of the
// contract): the right asked again for this project, the limits of IAM by
// the lists (#563; confirmed by IAM).
func (s *session) planFor(ctx context.Context, p project, aud Audience, days int) (plan, error) {
	pl := plan{p: p, aud: aud, days: days, profile: s.store.Profile(aud), previous: s.store.Previous(aud),
		term: s.cfg.Now().Add(time.Duration(days) * 24 * time.Hour).UTC().Truncate(time.Second)}
	// 12. Asked again for the chosen project, before any write.
	for _, a := range []string{auth.LoginActionCreateAccount, auth.LoginActionGrantRole} {
		ok, err := s.admin.Evaluate(ctx, iamadmin.Check{Action: a, ResourceID: p.ID})
		if err != nil {
			if err := s.uncheck(err); err != nil {
				return pl, at(stepEvaluate, err)
			}
			break
		}
		if !ok {
			return pl, at(stepEvaluate, noRight(p.Name))
		}
	}
	// 13. The account by its name: reused, its keys within the limit.
	sas, err := s.admin.ServiceAccounts(ctx, p.ID, aud.account())
	if err != nil {
		return pl, at(stepAccount, err)
	}
	if len(sas) > 0 {
		pl.saID = sas[0].ID
		keys, err := s.admin.AccessKeys(ctx, pl.saID)
		if err != nil {
			return pl, at(stepAccount, err)
		}
		if pl.keys = len(keys); pl.keys >= maxKeys {
			return pl, at(stepAccount, clierrors.New(clierrors.CodeUsage, eKeyLimit(aud.account(), pl.keys), eKeyLimitHint(aud.account())))
		}
		return pl, nil
	}
	all, err := s.admin.ServiceAccounts(ctx, p.ID, "")
	if err != nil {
		return pl, at(stepAccount, err)
	}
	if len(all) >= maxAccounts {
		return pl, at(stepAccount, clierrors.New(clierrors.CodeUsage, eAccountLimit(p.Name, len(all)), eAccountLimitHint()))
	}
	return pl, nil
}

// The limits of IAM, refused before any write (#563; confirmed by IAM):
// service accounts of a project, access keys of an account.
const (
	maxAccounts = 10
	maxKeys     = 50
)

// grant makes the planned access (steps 14–19), the key last; a failure
// after a write undoes what this login made, newest first (R-44). say tells
// each step done, in the terminal; nil on the page.
func (s *session) grant(ctx context.Context, pl plan, say func(string)) (Result, error) {
	if say == nil {
		say = func(string) {}
	}
	res, err := s.make(ctx, pl, say)
	if err != nil && len(s.made) > 0 {
		return res, s.rollback(ctx, err)
	}
	return res, err
}

func (s *session) make(ctx context.Context, pl plan, say func(string)) (Result, error) {
	aud, p := pl.aud, pl.p
	res := Result{Profile: pl.profile, ProjectID: p.ID, ProjectName: p.Name, Account: aud.account(), Role: auth.LoginRoleName}
	now := s.cfg.Now()
	desc := fmt.Sprintf("evo login, %s, %s", s.who.Login, now.UTC().Format("2006-01-02"))

	// 14. The account: the planned one, or created; a 409 is another
	// login's: list again and reuse.
	sa := pl.saID
	res.AccountReused = sa != ""
	if sa == "" {
		a, err := s.admin.CreateServiceAccount(ctx, p.ID, aud.account(), desc)
		switch {
		case err == nil:
			sa = a.ID
			s.made = append(s.made, undo{tUndoAccount(aud.account()),
				func(ctx context.Context) error { return s.admin.DeleteServiceAccount(ctx, a.ID) }})
		case iamadmin.StatusOf(err) == http.StatusConflict:
			again, lerr := s.admin.ServiceAccounts(ctx, p.ID, aud.account())
			if lerr != nil || len(again) == 0 {
				return res, at(stepAccount, err)
			}
			sa, res.AccountReused = again[0].ID, true
		default:
			return res, at(stepAccount, err)
		}
	}
	say(tStepAccount(aud.account(), res.AccountReused))

	// 15. The role until the key's term: a shorter one is moved forward,
	// a longer one kept, none created.
	if err := s.role(ctx, p, sa, pl.term); err != nil {
		return res, at(stepPermission, err)
	}
	say(tStepRole())

	// 16. The key, last.
	key, err := s.admin.CreateAccessKey(ctx, sa, desc, time.Duration(pl.days)*24*time.Hour)
	if err != nil {
		return res, at(stepKey, err)
	}
	s.made = append(s.made, undo{tUndoKey(),
		func(ctx context.Context) error { return s.admin.DeleteAccessKey(ctx, sa, key.ID) }})
	res.KeyID = key.KeyID
	res.KeyExpiresAt = pl.term
	if t, err := time.Parse(time.RFC3339, key.ExpiredAt); err == nil {
		res.KeyExpiresAt = t
	}
	say(tStepKey())

	// 17. The key works, by the exchange of every later command.
	if err := s.checkKey(ctx, key); err != nil {
		return res, at(stepKeyCheck, err)
	}
	say(tStepKeyCheck())

	// 18. The profile, at once (R-44); then the previous key goes.
	if err := s.store.Save(aud, Saved{KeyID: key.KeyID, Secret: key.Secret, ProjectID: p.ID, CustomerID: p.Customer,
		ServiceAccountID: sa, AccessKeyID: key.ID, Role: auth.LoginRoleName, Login: s.who.Login,
		KeyExpiresAt: res.KeyExpiresAt, LoginAt: now}); err != nil {
		return res, at(stepProfile, err)
	}
	s.made = nil // kept: nothing to undo now
	if prev := pl.previous; prev != nil && prev.AccessKeyID != "" && prev.AccessKeyID != key.ID {
		if err := s.admin.DeleteAccessKey(ctx, prev.ServiceAccountID, prev.AccessKeyID); err != nil && iamadmin.StatusOf(err) != http.StatusNotFound {
			res.PreviousNote = eKeyLeft(config.MaskKeyID(prev.KeyID), err)
		} else {
			res.RevokedKeyID = prev.KeyID
		}
	}
	return res, nil
}

// role grants the account the login role until term: a permission of a
// shorter term is moved forward, a longer one kept.
func (s *session) role(ctx context.Context, p project, sa string, term time.Time) error {
	perms, err := s.admin.RolePermissions(ctx, p.ID, sa, auth.LoginRoleName)
	if err != nil {
		return err
	}
	if len(perms) > 0 {
		have, perr := time.Parse(time.RFC3339, perms[0].ExpiresAt)
		if perms[0].ExpiresAt != "" && (perr != nil || have.Before(term)) {
			return s.admin.ExtendPermission(ctx, perms[0].ID, term)
		}
		return nil
	}
	g := iamadmin.Grant{SubjectID: sa, ResourceID: p.ID, RoleName: auth.LoginRoleName, ExpiresAt: term}
	perm, err := s.admin.CreatePermission(ctx, g)
	if iamadmin.StatusOf(err) == http.StatusBadRequest {
		// A custom role of the same name: the product role by its id.
		id, rerr := s.admin.RoleID(ctx, p.Customer, auth.LoginRoleName)
		if rerr != nil {
			return err
		}
		g.RoleName, g.RoleID = "", id
		perm, err = s.admin.CreatePermission(ctx, g)
	}
	if err != nil {
		return err
	}
	s.made = append(s.made, undo{tUndoRole(),
		func(ctx context.Context) error { return s.admin.DeletePermission(ctx, perm.ID) }})
	return nil
}

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
		return withStatus(clierrors.New(clierrors.CodeAuthFailed, eKeyCheck(status), eIDTokenHint()), status)
	}
	return nil
}

// rollback deletes what this login made, newest first, with the person's
// token. What it could not delete is named, with the way: the console. The
// cause's step and status stay in the details.
func (s *session) rollback(ctx context.Context, cause error) error {
	// The rollback runs even when the login's context is done.
	ctx = context.WithoutCancel(ctx)
	made := make([]string, 0, len(s.made))
	undone, left := []string{}, []string{}
	for i := len(s.made) - 1; i >= 0; i-- {
		made = append(made, s.made[i].what)
		if err := s.made[i].do(ctx); err != nil && iamadmin.StatusOf(err) != http.StatusNotFound {
			left = append(left, s.made[i].what)
		} else {
			undone = append(undone, s.made[i].what)
		}
	}
	s.made = nil
	slices.Reverse(made)
	code := clierrors.Reason(cause)
	if len(left) > 0 || !clierrors.IsCode(code) {
		code = clierrors.CodeAPIError
	}
	var why string
	details := map[string]any{"created": made, "undone": undone, "left": left}
	var ce *clierrors.CLIError
	if errors.As(cause, &ce) {
		why = ce.Message
		for k, v := range ce.Details {
			details[k] = v
		}
	} else {
		why = cause.Error()
	}
	out := clierrors.Wrap(code, eNotComplete(why, made, undone, left), eNotCompleteHint(len(left) > 0), cause)
	out.Details = details
	return out
}
