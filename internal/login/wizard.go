// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// byFlags is the plan of the flags alone (every one and --yes): no
// question; the summary goes to the terminal all the same.
func (s *session) byFlags(ctx context.Context) (plan, error) {
	// prepare kept only the projects --project names.
	if len(s.projects) > 1 {
		return plan{}, at(stepChoice, clierrors.New(clierrors.CodeUsage, eAmbiguous(s.choice.Project, len(s.projects)), eAmbiguousHint()))
	}
	p := s.projects[0]
	if !p.Allowed {
		return plan{}, at(stepChoice, noRight(p.Name))
	}
	pl, err := s.planFor(ctx, p, s.choice.For, s.choice.Days)
	if err != nil {
		return pl, err
	}
	s.summary(pl)
	return pl, nil
}

// wizard asks in the terminal what the flags left open: the organization,
// the project, whom the key is for and its term; then the summary and
// «Continue? [y/N]», unless --yes.
func (s *session) wizard(ctx context.Context) (plan, error) {
	// The organizations, in the order of the list.
	var orgs []project
	for _, p := range s.projects {
		if len(orgs) == 0 || orgs[len(orgs)-1].Customer != p.Customer {
			orgs = append(orgs, p)
		}
	}
	org := orgs[0].Customer
	if len(orgs) > 1 {
		s.tell(tOrganizations())
		var opts []option
		for _, o := range orgs {
			opts = append(opts, option{id: o.Customer, line: fmt.Sprintf("%s (%s)", o.CustomerName, o.Customer), ok: true})
		}
		i, err := s.pick(ctx, tChooseOrg(), opts)
		if err != nil {
			return plan{}, err
		}
		org = orgs[i].Customer
	}

	var ps []project
	for _, p := range s.projects {
		if p.Customer == org {
			ps = append(ps, p)
		}
	}
	s.tell(tProjectsAdmin())
	var opts []option
	for _, p := range ps {
		line := fmt.Sprintf("%s (%s)", p.Name, p.ID)
		if !p.Allowed {
			line += " — " + tNoRightMark()
		}
		opts = append(opts, option{id: p.ID, line: line, ok: p.Allowed})
	}
	i, err := s.pick(ctx, tChooseProject(), opts)
	if err != nil {
		return plan{}, err
	}
	p := ps[i]

	aud := s.choice.For
	if aud == "" {
		s.tell(tForWhom())
		i, err := s.pick(ctx, tChoose(), []option{
			{id: string(ForMe), line: tForMe() + " — " + tForMeNote(), ok: true},
			{id: string(ForAgent), line: tForAgent() + " — " + tForAgentNote(), ok: true, def: true},
		})
		if err != nil {
			return plan{}, err
		}
		s.tell(tRoleNote())
		aud = []Audience{ForMe, ForAgent}[i]
	}

	days := s.choice.Days
	for tries := 0; days == 0; tries++ {
		a, err := s.cfg.Ask(ctx, fmt.Sprintf("%s [%d]: ", tTermQuestion(), DefaultTerm))
		if err != nil {
			return plan{}, err
		}
		a = strings.TrimSuffix(strings.TrimSpace(a), "d")
		if a == "" {
			days = DefaultTerm
		} else if n, err := strconv.Atoi(a); err == nil && containsInt(Terms, n) {
			days = n
		} else if tries == 2 {
			return plan{}, at(stepChoice, clierrors.New(clierrors.CodeUsage, eChoice("ttl"), tForeverInConsole()))
		}
	}

	pl, err := s.planFor(ctx, p, aud, days)
	if err != nil {
		return pl, err
	}
	s.summary(pl)
	if s.choice.Yes {
		return pl, nil
	}
	a, err := s.cfg.Ask(ctx, tContinue()+" [y/N]: ")
	if err != nil {
		return pl, err
	}
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "y", "yes", "д", "да":
		return pl, nil
	}
	return pl, cancelled()
}

// summary is what will be done, in the words of the page.
func (s *session) summary(pl plan) {
	acc := tAccountIn(pl.aud.account(), pl.p.Name)
	if pl.saID != "" {
		acc = tAccountThere(pl.aud.account(), pl.p.Name, pl.keys, maxKeys)
	}
	s.tell(tWhatWillBeDone())
	rows := [][2]string{{tRowAccount(), acc}, {tRowRole(), tRoleUntil(pl.term)}, {tRowKey(), tKeyDays(pl.days, pl.term)},
		{tRowProfile(), pl.profile}}
	if pl.previous != nil && pl.previous.AccessKeyID != "" {
		rows = append(rows, [2]string{tRevokeRow(), tRevokeValue(config.MaskKeyID(pl.previous.KeyID))})
	}
	for _, r := range rows {
		s.tell(fmt.Sprintf("  %-14s %s", r[0], r[1]))
	}
	s.tell(tSumStays())
}

// option is one line of a question of the wizard: chosen by its number or
// its id; one the person may not choose is shown, not taken.
type option struct {
	id, line string
	ok, def  bool
}

// pick asks for one of opts, three times at most. The default is the
// option marked so, or the first one that may be chosen.
func (s *session) pick(ctx context.Context, question string, opts []option) (int, error) {
	def := -1
	for i, o := range opts {
		s.tell(fmt.Sprintf("  %d. %s", i+1, o.line))
		if o.ok && (o.def || def < 0) {
			def = i
		}
	}
	if def < 0 {
		return 0, at(stepChoice, noRight(opts[0].line))
	}
	for tries := 0; tries < 3; tries++ {
		a, err := s.cfg.Ask(ctx, fmt.Sprintf("%s [%d]: ", question, def+1))
		if err != nil {
			return 0, err
		}
		a = strings.TrimSpace(a)
		i := -1
		if a == "" {
			i = def
		} else if n, err := strconv.Atoi(a); err == nil && n >= 1 && n <= len(opts) {
			i = n - 1
		} else {
			for j, o := range opts {
				if o.id == a {
					i = j
				}
			}
		}
		if i >= 0 && opts[i].ok {
			return i, nil
		}
		if i >= 0 {
			s.tell(tNoRightNote())
		}
	}
	return 0, at(stepChoice, clierrors.New(clierrors.CodeUsage, eChoice(question), eRetry()))
}
