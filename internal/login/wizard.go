// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// wizard gathers in the terminal, before the browser, what the flags left
// open: whom the key is for and its term; then the summary. It grants
// nothing: a pseudo-terminal is any process's («Риск», conclusion 24), so
// access is the page's «Grant access» alone, where this choice is offered
// chosen and the project is picked.
func (s *session) wizard(ctx context.Context) (Choice, error) {
	c := s.choice
	if c.For == "" {
		s.tell(tForWhom())
		i, err := s.pick(ctx, tChoose(), []option{
			{id: string(ForMe), line: tForMe() + " — " + tForMeNote()},
			{id: string(ForAgent), line: tForAgent() + " — " + tForAgentNote(), def: true},
		})
		if err != nil {
			return c, err
		}
		s.tell(tRoleNote())
		c.For = []Audience{ForMe, ForAgent}[i]
	}
	for tries := 0; c.Days == 0; tries++ {
		a, err := s.cfg.Ask(ctx, fmt.Sprintf("%s [%d]: ", tTermQuestion(), DefaultTerm))
		if err != nil {
			return c, err
		}
		a = strings.TrimSuffix(strings.TrimSpace(a), "d")
		if a == "" {
			c.Days = DefaultTerm
		} else if n, err := strconv.Atoi(a); err == nil && containsInt(Terms, n) {
			c.Days = n
		} else if tries == 2 {
			return c, at(stepChoice, clierrors.New(clierrors.CodeUsage, eChoice("ttl"), tForeverInConsole()))
		}
	}
	term := s.cfg.Now().Add(time.Duration(c.Days) * 24 * time.Hour)
	s.tell(tWhatWillBeDone())
	for _, r := range [][2]string{{tRowAccount(), c.For.account()}, {tRowRole(), tRoleUntil(term)},
		{tRowKey(), tKeyDays(c.Days, term)}, {tRowProfile(), s.store.Profile(c.For)}} {
		s.tell(fmt.Sprintf("  %-14s %s", r[0], r[1]))
	}
	s.tell(tSumStays())
	s.tell(tGrantOnPage())
	return c, nil
}

// option is one line of a question of the wizard: chosen by its number or
// its id.
type option struct {
	id, line string
	def      bool
}

// pick asks for one of opts, three times at most; the default is the
// option marked so.
func (s *session) pick(ctx context.Context, question string, opts []option) (int, error) {
	def := 0
	for i, o := range opts {
		s.tell(fmt.Sprintf("  %d. %s", i+1, o.line))
		if o.def {
			def = i
		}
	}
	for tries := 0; tries < 3; tries++ {
		a, err := s.cfg.Ask(ctx, fmt.Sprintf("%s [%d]: ", question, def+1))
		if err != nil {
			return 0, err
		}
		a = strings.TrimSpace(a)
		if a == "" {
			return def, nil
		}
		if n, err := strconv.Atoi(a); err == nil && n >= 1 && n <= len(opts) {
			return n - 1, nil
		}
		for j, o := range opts {
			if o.id == a {
				return j, nil
			}
		}
	}
	return 0, at(stepChoice, clierrors.New(clierrors.CodeUsage, eChoice(question), eRetry()))
}
