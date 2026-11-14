// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// goingOnReads records the reads goingOn makes: the deadline of each and
// whether it was sent once.
type goingOnReads struct {
	mu        sync.Mutex
	deadlines []time.Time
	once      []bool
	hung      bool
}

func (g *goingOnReads) saw(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	d, _ := ctx.Deadline()
	g.deadlines = append(g.deadlines, d)
	g.once = append(g.once, httpretry.IsOnce(ctx))
}

// hang waits for the end of ctx; a read the limit does not end is hung.
func (g *goingOnReads) hang(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("read: %w", ctx.Err())
	case <-time.After(30 * time.Second):
		g.mu.Lock()
		g.hung = true
		g.mu.Unlock()
		return errors.New("hung")
	}
}

func shortGoingOnLimit(t *testing.T) {
	t.Helper()
	old := goingOnLimit
	goingOnLimit = 50 * time.Millisecond
	t.Cleanup(func() { goingOnLimit = old })
}

// The write is made and paid for: the read of the state goingOn adds is
// one attempt within one limit for all its reads, and a read that hangs
// does not hold the answer with the id — its failure is named («Риск»,
// conclusion 242, У1). k8s_add_nodepool's reads are two: k8s_get of the
// pool's id (404) and k8s_nodepool_get; the limit is theirs together.
func TestGoingOn_TheReadOfTheStateIsBounded(t *testing.T) {
	shortGoingOnLimit(t)
	g := &goingOnReads{}
	s := &Registry{handlers: map[string]Handler{
		"k8s_get": func(ctx context.Context, _ map[string]any) (string, bool) {
			g.saw(ctx)
			return `{"error":"not found","reason":"NOT_FOUND"}`, true
		},
		"k8s_nodepool_get": func(ctx context.Context, _ map[string]any) (string, bool) {
			g.saw(ctx)
			err := g.hang(ctx)
			return `{"error":"` + err.Error() + `","reason":"` + clierrors.Reason(err) + `"}`, true
		},
	}}
	ans := map[string]any{"id": "pool-1", "operation_id": "op-1"}
	s.goingOn(context.Background(), ans, "k8s_operation", "op-1", "pool-1", nil)
	if g.hung {
		t.Fatal("the read of the state hung past its limit: the answer with the id waited for it")
	}
	if len(g.deadlines) != 2 || g.deadlines[0].IsZero() || !g.deadlines[0].Equal(g.deadlines[1]) || !g.once[0] || !g.once[1] {
		t.Errorf("reads: deadlines %v, once %v; want two reads under one deadline, each sent once", g.deadlines, g.once)
	}
	if ans["id"] != "pool-1" || ans["next_tool"] != "k8s_operation" || ans["resource_status_error"] == nil || ans["resource_status"] != nil {
		t.Errorf("answer %v: want the id, the next step and resource_status_error", ans)
	}

	// A resource with no read of its own: the read of the list, the same.
	g = &goingOnReads{}
	ans = map[string]any{"id": "addon-1", "operation_id": "op-2"}
	s.goingOn(context.Background(), ans, "k8s_operation", "op-2", "addon-1", func(ctx context.Context) (map[string]any, error) {
		g.saw(ctx)
		return nil, g.hang(ctx)
	})
	if g.hung || len(g.once) != 1 || !g.once[0] || g.deadlines[0].IsZero() {
		t.Errorf("the read of the list: hung %v, once %v, deadlines %v; want one read sent once, under the limit", g.hung, g.once, g.deadlines)
	}
	if ans["id"] != "addon-1" || ans["resource_status_error"] == nil {
		t.Errorf("answer %v: want the id and resource_status_error", ans)
	}
}

// Right after a create, reads that do not find the resource do not say it
// is deleted — it may be on its way — and the answer says not to create it
// again: a repeat of a paid create is a second resource (У2). Both ways of
// reading the state say the same.
func TestGoingOn_NotFoundAfterACreateIsNotDeleted(t *testing.T) {
	notFound := func(context.Context, map[string]any) (string, bool) {
		return `{"error":"not found","reason":"NOT_FOUND"}`, true
	}
	s := &Registry{handlers: map[string]Handler{"pg_get": notFound}}
	byID := map[string]any{}
	s.goingOn(context.Background(), byID, "pg_operation", "op-1", "cluster-1", nil)
	listed := map[string]any{}
	s.goingOn(context.Background(), listed, "pg_operation", "op-2", "", func(context.Context) (map[string]any, error) { return nil, nil })
	for way, ans := range map[string]map[string]any{"read by id": byID, "read of the list": listed} {
		note, _ := ans["resource_status_note"].(string)
		low := strings.ToLower(note)
		if ans["resource_status"] != "not_found" || strings.Contains(low, "delet") || strings.Contains(low, "удал") ||
			!(strings.Contains(low, "do not create it again") || strings.Contains(low, "не создавайте его повторно")) {
			t.Errorf("%s: resource_status %v, note %q; want not_found, no «deleted», and «do not create it again»", way, ans["resource_status"], note)
		}
	}
}
