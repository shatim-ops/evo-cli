// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package floatingip

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// fakeCloud is a cloud of floating IPs: create answers err (made: the
// address exists all the same), the list hides what was made for lag
// reads.
type fakeCloud struct {
	provision.CloudAPI
	err     error
	made    bool
	lag     int
	creates int
	lists   int
	deleted []string
	fips    []*provision.FloatingIP
}

func (c *fakeCloud) CreateFloatingIP(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.creates++
	f := &provision.FloatingIP{ID: "fip-" + req.Name, Name: req.Name, IPAddress: "203.0.113.5", State: "available"}
	if c.err != nil {
		if c.made {
			c.fips = append(c.fips, f)
		}
		return nil, c.err
	}
	c.fips = append(c.fips, f)
	return f, nil
}

func (c *fakeCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	c.lists++
	if c.lag > 0 {
		c.lag--
		return nil, nil
	}
	return c.fips, nil
}

func (c *fakeCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.deleted = append(c.deleted, id)
	return nil
}

func status(code int) error {
	return clerrors.Wrap(clerrors.CodeAPIError, "x", "", &provision.StatusError{Status: code})
}

var req = provision.CreateFloatingIPRequest{Name: "fip-web-1", ProjectID: "p"}

// One create, whatever its outcome: a refusal is returned for the caller
// to send again (nothing was made); no answer is looked up by the exact
// name, and one the list does not show is a LostError — never a second
// create (D107; «Риск», conclusion 180, У1).
func TestReserve_SendsOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		made, lag bool
		want      string // "ok", "refused", "lost", "found"
	}{
		{"answered", nil, false, false, "ok"},
		{"refused 409", status(409), false, false, "refused"},
		{"quota", clerrors.Wrap(clerrors.CodeAPIError, "x", "", &provision.StatusError{Status: 403, Body: "quota exceeded"}), false, false, "refused"},
		{"503 made and listed", status(503), true, false, "found"},
		{"503 made, list behind", status(503), true, true, "lost"},
		{"408 not made", status(408), false, false, "lost"},
		{"transport", errors.New("connection reset"), false, false, "lost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &fakeCloud{err: tc.err, made: tc.made}
			if tc.lag {
				c.lag = 10
			}
			f, err := Reserve(context.Background(), c, req, Free, time.Millisecond)
			got := "ok"
			switch _, lost := AsLost(err); {
			case lost:
				got = "lost"
			case err != nil:
				got = "refused"
			case tc.made:
				got = "found"
			}
			if got != tc.want || c.creates != 1 {
				t.Fatalf("got %s (f %v, err %v) after %d creates; want %s after 1", got, f, err, c.creates, tc.want)
			}
		})
	}
}

// К1: the address is taken by its exact name, bound to nothing or to the
// VM; one of another VM is not.
func TestFind_ExactNameAndOwner(t *testing.T) {
	bound := func(id string) json.RawMessage { return json.RawMessage(`{"id":"` + id + `"}`) }
	c := &fakeCloud{fips: []*provision.FloatingIP{
		{ID: "a", Name: "fip-web-1x"},
		{ID: "b", Name: "fip-web-1", VM: bound("vm-other"), State: "in_use"},
	}}
	if f := Find(context.Background(), c, "p", "fip-web-1", FreeOrOf("vm-1")); f != nil {
		t.Fatalf("took %s: a prefix, or an address of another VM", f.ID)
	}
	c.fips = append(c.fips, &provision.FloatingIP{ID: "c", Name: "fip-web-1", VM: bound("vm-1"), State: "in_use"})
	if f := Find(context.Background(), c, "p", "fip-web-1", FreeOrOf("vm-1")); f == nil || f.ID != "c" {
		t.Fatalf("got %v, want the address on vm-1", f)
	}
}

// The repeat: the address of the mark, listed, is taken and its mark
// goes; not listed — the question, until a person answered; answered —
// the call goes on and the mark stays (Т4).
func TestSettle_FindsThenAsks(t *testing.T) {
	mark := state.LostFloatingIP{Name: "fip-web-1", ProjectID: "p", VMName: "web", RequestedAt: time.Unix(1, 0)}
	sf := &state.File{}
	Note(sf, mark)
	c := &fakeCloud{}
	if _, err := Settle(context.Background(), c, sf, "p", "web", "", Free); err == nil {
		t.Fatal("not listed, not answered: want the question")
	} else if ask, ok := AsAsk(err); !ok || ask.Mark.Name != "fip-web-1" {
		t.Fatalf("err %v, want an AskError of fip-web-1", err)
	}
	Answer(sf, "p", "fip-web-1")
	if f, err := Settle(context.Background(), c, sf, "p", "web", "", Free); f != nil || err != nil || len(sf.LostFloatingIPs) != 1 {
		t.Fatalf("answered: f %v err %v marks %v; want nothing and the mark kept", f, err, sf.LostFloatingIPs)
	}
	c.fips = []*provision.FloatingIP{{ID: "fip-1", Name: "fip-web-1", State: "available"}}
	if f, err := Settle(context.Background(), c, sf, "p", "web", "", Free); err != nil || f == nil || f.ID != "fip-1" || len(sf.LostFloatingIPs) != 0 {
		t.Fatalf("listed: f %v err %v marks %v; want fip-1 and no mark", f, err, sf.LostFloatingIPs)
	}
	if c.creates != 0 {
		t.Fatalf("Settle sent %d creates", c.creates)
	}
}

// destroy: a listed address is released, one not listed is named and its
// mark closes (Т3, Т4).
func TestRelease_ReleasesOrNames(t *testing.T) {
	sf := &state.File{}
	Note(sf, state.LostFloatingIP{Name: "fip-a", ProjectID: "p", VMName: "web", VMID: "vm-1"})
	Note(sf, state.LostFloatingIP{Name: "fip-b", ProjectID: "p", VMName: "web"})
	Note(sf, state.LostFloatingIP{Name: "fip-c", ProjectID: "p", VMName: "other"})
	c := &fakeCloud{fips: []*provision.FloatingIP{{ID: "id-a", Name: "fip-a", State: "available"}}}
	rel, miss, err := Release(context.Background(), c, sf, "p", "web", "vm-1")
	if err != nil || len(rel) != 1 || rel[0].Name != "fip-a" || len(miss) != 1 || miss[0].Name != "fip-b" {
		t.Fatalf("released %v missing %v err %v", rel, miss, err)
	}
	if len(c.deleted) != 1 || c.deleted[0] != "id-a" || len(sf.LostFloatingIPs) != 1 || sf.LostFloatingIPs[0].Name != "fip-c" {
		t.Fatalf("deleted %v, marks %v; want id-a and only the other VM's mark", c.deleted, sf.LostFloatingIPs)
	}
}

// Apply keeps the marks another process wrote between the read and the
// write.
func TestApply_KeepsOthers(t *testing.T) {
	a := state.LostFloatingIP{Name: "a", ProjectID: "p"}
	b := state.LostFloatingIP{Name: "b", ProjectID: "p"}
	other := state.LostFloatingIP{Name: "o", ProjectID: "p"}
	disk := &state.File{LostFloatingIPs: []state.LostFloatingIP{a, other}}
	bAnswered := b
	bAnswered.Answered = true
	Apply(disk, []state.LostFloatingIP{a}, []state.LostFloatingIP{bAnswered})
	got := map[string]bool{}
	for _, m := range disk.LostFloatingIPs {
		got[m.Name] = m.Answered
	}
	if len(got) != 2 || !got["b"] {
		t.Fatalf("marks %+v; want o kept, a gone, b answered", disk.LostFloatingIPs)
	}
	if _, ok := got["o"]; !ok {
		t.Fatalf("the mark another process wrote is lost: %+v", disk.LostFloatingIPs)
	}
}
