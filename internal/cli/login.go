// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/login"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// loginConfig is the login's Config; the tests point it at the world of
// B-2 (internal/loginworld).
var loginConfig = login.Default

// loginSay tells the address to open on stderr: two lines of text, or one
// JSON line with --json (stdout keeps the one answer).
func loginSay(cmd *cobra.Command) func(message, url string) {
	return func(message, u string) {
		if flagJSON {
			_ = json.NewEncoder(cmd.ErrOrStderr()).Encode(map[string]string{"message": message, "url": u})
			return
		}
		fmt.Fprintln(cmd.ErrOrStderr(), message)
		fmt.Fprintln(cmd.ErrOrStderr(), u)
	}
}

// openBrowser opens a URL in the person's browser; the tests replace it.
var openBrowser = func(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}

func newLoginCmd() *cobra.Command {
	var project, forWhom, ttl string
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: login.TLoginShort(),
		Long:  login.TLoginLong(),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flagYes {
				return login.RefuseYes()
			}
			if flagDryRun {
				return login.RefuseDryRun()
			}
			for _, n := range []string{"project-id", "region"} {
				if f := cmd.Flag(n); f != nil && f.Changed {
					return login.RefuseScopeFlag(n)
				}
			}
			// --for '' is no audience: not the default one (#1095).
			if cmd.Flags().Changed("for") && forWhom == "" {
				return clerrors.New(clerrors.CodeUsage, login.EFor(forWhom), "")
			}
			choice, err := loginChoice(project, forWhom, ttl)
			if err != nil {
				return err
			}
			store := loginStore{}
			if name, explicit := config.ProfileName(flagProfile); explicit {
				if err := config.ValidateProfileName(name); err != nil {
					return err
				}
				store.name = name
			}
			// The agent's profile takes no key for the person (C24): refused
			// here for --for me; the page offers the agent, and the choice of
			// «Мне» there is refused before any write (loginStore.Refuses).
			if store.name == agentProfile {
				if choice.For == login.ForMe {
					return clerrors.New(clerrors.CodeUsage, login.EAgentProfileForMe(store.name), login.EAgentProfileForMeHint())
				}
				choice.For = login.ForAgent
			}
			if err := writableConfigDir(); err != nil {
				return err
			}
			choice.Preselect, choice.PreselectFrom, choice.PreselectEnv = loginPreselectFor(store, choice.For)
			cfg := loginConfig(loginSay(cmd), openBrowser)
			if noBrowser {
				cfg.Open = nil
			}
			cfg.Tell = loginTell(cmd)
			// The intent of the key lives where evo doctor and the next
			// run read the intents: the working directory (№ 37, R-59).
			if wd, err := os.Getwd(); err == nil {
				cfg.IntentRoot = wd
			}
			warnBeforeLogin(cfg.Tell, store, choice.For)
			res, err := login.Login(cmd.Context(), cfg, choice, store)
			if err != nil {
				return err
			}
			printer := output.New(flagJSON, flagNoColor)
			var revoked any
			if res.RevokedKeyID != "" {
				revoked = config.MaskKeyID(res.RevokedKeyID)
			}
			out := map[string]any{
				"profile": res.Profile, "key_id": config.MaskKeyID(res.KeyID), "project_id": res.ProjectID,
				"project_name": res.ProjectName, "service_account": res.Account, "role": res.Role,
				"key_expires_at": res.KeyExpiresAt.UTC().Format(time.RFC3339), "reused_account": res.AccountReused,
				"revoked_key_id": revoked, "role_unbounded": res.RoleUnbounded,
			}
			// An unbounded role has no term to name (A18-26).
			if !res.RoleUnbounded {
				out["role_expires_at"] = res.RoleExpiresAt.UTC().Format(time.RFC3339)
			}
			if res.PreviousNote != "" {
				out["warning"] = res.PreviousNote
			}
			if res.Unchecked != "" {
				out["unchecked"] = res.Unchecked
			}
			if printer.IsJSON() {
				return printer.JSON(out)
			}
			printer.Success(login.TSignedInCLI(config.MaskKeyID(res.KeyID), res.Account, res.ProjectName, res.Profile, res.KeyExpiresAt))
			printer.Hint(login.TResultRole(res.RoleExpiresAt, res.RoleUnbounded))
			if res.AccountReused {
				prev, _ := revoked.(string)
				printer.Hint(login.TResultReused(res.Account, prev))
			}
			printer.Hint(login.TResultCheck(res.Profile))
			if res.For == login.ForMe {
				printer.Hint(login.TResultForAgent())
			}
			if res.PreviousNote != "" {
				printer.Warning(res.PreviousNote)
			}
			return nil
		},
	}
	// evo login takes no --yes (R-47): its help does not offer the global
	// one (#1095 (о)); the flag is shown again for the other commands.
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		if f := c.InheritedFlags().Lookup("yes"); f != nil {
			f.Hidden = true
			defer func() { f.Hidden = false }()
		}
		c.Parent().HelpFunc()(c, args)
	})
	cmd.Flags().StringVar(&project, "project", "", login.TFlagProject())
	cmd.Flags().StringVar(&forWhom, "for", "", login.TFlagFor())
	cmd.Flags().StringVar(&ttl, "key-ttl", "", login.TFlagTTL())
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, login.TFlagNoBrowser())
	return cmd
}

// loginPreselect is the project the consent page offers checked (R-48):
// CLOUDRU_PROJECT_ID, as for every command, else the project of the
// profile of now; with where it came from. --project filters the list and
// wins over both.
// loginPreselectFor is the project picked beforehand from the profile the
// key goes to: --for agent reads the profile agent, not default (#1095
// (п)); with no --for, the page's default «Мне».
func loginPreselectFor(s loginStore, a login.Audience) (id, from string, env bool) {
	if a == "" {
		a = login.ForMe
	}
	return loginPreselect(s.Profile(a))
}

func loginPreselect(profileFlag string) (id, from string, env bool) {
	if env := strings.TrimSpace(os.Getenv("CLOUDRU_PROJECT_ID")); env != "" {
		return env, login.TFromEnv(), true
	}
	name, _ := config.ProfileName(profileFlag)
	if prof, _, found, err := config.LoadProfile(name); err == nil && found && prof.ProjectID != "" {
		return prof.ProjectID, login.TFromProfile(name), false
	}
	return "", "", false
}

// agentProfile is the profile of a key for an agent, unless --profile or
// CLOUDRU_PROFILE names one.
const agentProfile = "agent"

// loginStore keeps the key of evo login in a profile: the one named, or
// the one of the audience.
type loginStore struct{ name string }

func (s loginStore) Profile(a login.Audience) string {
	switch {
	case s.name != "":
		return s.name
	case a == login.ForAgent:
		return agentProfile
	}
	return config.DefaultProfile
}

// Refuses the person's key for the agent's profile (C24).
func (s loginStore) Refuses(a login.Audience) error {
	if s.name == agentProfile && a == login.ForMe {
		return clerrors.New(clerrors.CodeUsage, login.EAgentProfileForMe(s.name), login.EAgentProfileForMeHint())
	}
	return nil
}

// writableConfigDir checks, before the browser, that the key can be saved:
// the directory of the profiles is made and written to (#1095 (б)).
func writableConfigDir() error {
	dir := config.Dir()
	err := config.MkdirPrivate(dir)
	if err == nil {
		var f *os.File
		if f, err = os.CreateTemp(dir, ".evo-login-*"); err == nil {
			name := f.Name()
			_ = f.Close()
			_ = os.Remove(name)
		}
	}
	if err != nil {
		return clerrors.Wrap(clerrors.CodeConfigInvalid, login.EConfigDir(dir, err), login.EConfigDirHint(), err)
	}
	return nil
}

// warnBeforeLogin names, before the browser, a key the profile already has
// and keys in the environment, which come before the profile (#1095 (и)).
func warnBeforeLogin(tell func(string), s loginStore, a login.Audience) {
	if a == "" {
		a = login.ForMe
	}
	name := s.Profile(a)
	if _, creds, _, err := config.LoadProfile(name); err == nil && creds.KeyID != "" {
		tell(login.TProfileHasKey(name))
	}
	if os.Getenv("CLOUDRU_KEY_ID") != "" {
		tell(login.TEnvKeyWins())
	}
}

func (s loginStore) Previous(a login.Audience) *login.Previous {
	_, creds, _, err := config.LoadProfile(s.Profile(a))
	if err != nil || creds.Login == nil {
		return nil
	}
	return &login.Previous{ServiceAccountID: creds.Login.ServiceAccountID, AccessKeyID: creds.Login.AccessKeyID, KeyID: creds.KeyID}
}

func (s loginStore) Save(a login.Audience, k login.Saved) error {
	name := s.Profile(a)
	prof, creds, _, err := config.LoadProfile(name)
	if err != nil {
		return err
	}
	prof.ProjectID = k.ProjectID
	creds.KeyID, creds.Secret = k.KeyID, k.Secret
	creds.Login = &config.LoginKey{ServiceAccountID: k.ServiceAccountID, AccessKeyID: k.AccessKeyID,
		KeyExpiresAt: k.KeyExpiresAt.UTC().Format(time.RFC3339), CustomerID: k.CustomerID, Role: k.Role,
		LoggedInAs: k.Login, LoginAt: k.LoginAt.UTC().Format(time.RFC3339)}
	return config.SaveProfile(name, prof, creds)
}

// loginTell writes a line of the login on stderr: text, or one JSON line
// with --json.
func loginTell(cmd *cobra.Command) func(string) {
	return func(line string) {
		if flagJSON {
			_ = json.NewEncoder(cmd.ErrOrStderr()).Encode(map[string]string{"message": line})
			return
		}
		fmt.Fprintln(cmd.ErrOrStderr(), line)
	}
}

// loginChoice is the choice of the flags; a bad one is USAGE before any
// request.
func loginChoice(project, forWhom, ttl string) (login.Choice, error) {
	c := login.Choice{Project: strings.TrimSpace(project), For: login.Audience(forWhom)}
	// A project is an ID or a name: not blank, no control character, not
	// longer than 128 characters — refused before the browser (#1095).
	if project != "" && (c.Project == "" || utf8.RuneCountInString(c.Project) > 128 ||
		strings.IndexFunc(c.Project, unicode.IsControl) >= 0) {
		return c, clerrors.New(clerrors.CodeUsage, login.EProject(), "")
	}
	if forWhom != "" && c.For != login.ForMe && c.For != login.ForAgent {
		return c, clerrors.New(clerrors.CodeUsage, login.EFor(forWhom), "")
	}
	if ttl != "" {
		// The term is «7d», «30d» or «90d»: «7» alone is refused, as the
		// help names the values with «d» (plan 1.8, #1087).
		days, hasD := strings.CutSuffix(ttl, "d")
		n, err := strconv.Atoi(days)
		ok := err == nil && hasD
		if ok {
			ok = false
			for _, d := range login.Terms {
				ok = ok || d == n
			}
		}
		if !ok {
			return c, clerrors.New(clerrors.CodeUsage, login.ETTL(ttl),
				login.ETTLHint())
		}
		c.Days = n
	}
	return c, nil
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: login.TLogoutShort(),
		Long:  login.TLogoutLong(),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := config.ProfileName(flagProfile)
			_, creds, found, err := config.LoadProfile(name)
			if err != nil {
				return err
			}
			if !found {
				return clerrors.New(clerrors.CodeConfigInvalid, login.ENoProfile(name),
					login.ENoProfileHint())
			}
			printer := output.New(flagJSON, flagNoColor)
			summary := login.TLogoutDelete(name)
			if creds.Login != nil {
				summary = login.TLogoutRevoke(config.MaskKeyID(creds.KeyID), name)
			}
			ok, err := confirm.Ask(cmd.Context(), confirm.Preview{Action: "logout", Summary: summary,
				Details: map[string]any{"profile": name, "key_id": config.MaskKeyID(creds.KeyID)}},
				confirm.Options{Yes: flagYes, DryRun: flagDryRun, JSON: flagJSON, Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()})
			if !ok {
				return err
			}
			out := map[string]any{"profile": name}
			if creds.Login != nil {
				provider := auth.NewIAMProvider(auth.Credentials{KeyID: creds.KeyID, Secret: creds.Secret}, httpClient, nil)
				res, err := login.Logout(cmd.Context(), loginConfig(loginSay(cmd), openBrowser).AdminBase, provider,
					config.MaskKeyID(creds.KeyID), creds.Login.ServiceAccountID, creds.Login.AccessKeyID)
				if err != nil {
					return err
				}
				out["key"] = res.Key
				switch {
				case res.Key == login.KeyRejected:
					out["key_id"], out["access_key_id"] = config.MaskKeyID(creds.KeyID), creds.Login.AccessKeyID
					out["note"] = login.TLogoutRejected(config.MaskKeyID(creds.KeyID), creds.Login.AccessKeyID)
				case !res.TokensUntil.IsZero():
					out["tokens_valid_until"] = res.TokensUntil.UTC().Format(time.RFC3339)
					out["note"] = login.TLogoutTokensUntil(res.TokensUntil)
				}
			} else {
				out["key"] = "kept"
				out["note"] = login.TLogoutKept()
			}
			if err := config.DeleteProfile(name); err != nil {
				return err
			}
			out["deleted"] = name
			if printer.IsJSON() {
				return printer.JSON(out)
			}
			printer.Success(login.TLogoutDone(name, out["key"]))
			if n, ok := out["note"].(string); ok {
				printer.Warning(n)
			}
			return nil
		},
	}
}
