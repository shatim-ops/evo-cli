// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// provision returns the password evo generated for the image once (dogfood
// 1.2.1, feedback 3). Over HTTP without --allow-secret-export H7 masks it,
// as deploy_apply's; over stdio and with the flag it is there. The ssh
// command is not a credential: it stays. All IDs and addresses are made up.

// provisionCloud is what one provision with a boot floating IP reads and
// makes; it records the create request.
type provisionCloud struct {
	provision.CloudAPI
	mu  sync.Mutex
	req provision.CreateVMRequest
}

func (*provisionCloud) SetToken(string) {}

// ListDiskTypes: the plan prices the preview before the question (D35);
// the price is not this fake's concern.
func (*provisionCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return nil, errors.New("no disk types in this fake")
}
func (*provisionCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return nil, nil
}
func (*provisionCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{{ID: "00000000-0000-4000-8000-0000000000f1", Name: "lowcost10-1-1", VCPUs: 1, RAMGB: 1}}, nil
}
func (*provisionCloud) GetImage(context.Context, string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: "00000000-0000-4000-8000-0000000000a1", Name: "Ubuntu-24.04", MetadataFields: []provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"},
		{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"},
		{Name: "linux_password", FieldType: "linux_password", MandatoryGroup: "authentication", Sensitive: true},
	}}, nil
}
func (*provisionCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return []*provision.Image{{ID: "00000000-0000-4000-8000-0000000000a1", Name: "Ubuntu-24.04"}}, nil
}
func (*provisionCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{{ID: "00000000-0000-4000-8000-0000000000b1", Name: "default"}}, nil
}
func (*provisionCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "00000000-0000-4000-8000-0000000000c1", Name: "default", Default: true}}, nil
}
func (*provisionCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, nil
}
func (*provisionCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return nil, nil
}
func (*provisionCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	return &provision.FloatingIP{ID: "00000000-0000-4000-8000-0000000000d1", IPAddress: "203.0.113.21", State: "available"}, nil
}
func (*provisionCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}
func (*provisionCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	return &provision.FloatingIP{ID: "00000000-0000-4000-8000-0000000000d1", IPAddress: "203.0.113.21", State: "in_use"}, nil
}
func (c *provisionCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.mu.Lock()
	c.req = req
	c.mu.Unlock()
	return &provision.VM{ID: "00000000-0000-4000-8000-0000000000e1", Status: "creating"}, nil
}
func (*provisionCloud) GetVM(_ context.Context, id string) (*provision.VM, error) {
	return &provision.VM{ID: id, Status: "active"}, nil
}

func (c *provisionCloud) password() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.req.ImageMetadata["linux_password"]
}

// provisionServer is an MCP server whose provision runs on cloud in a
// project directory of its own.
func provisionServer(t *testing.T, cloud *provisionCloud) *Server {
	t.Helper()
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    config.Config{ProjectID: "00000000-0000-4000-8000-000000000001", Region: "00000000-0000-4000-8000-000000000002", ImageID: "00000000-0000-4000-8000-0000000000a1", KeyID: "k", Secret: "s"},
			Auth:      breakToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("fake-key"),
			SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
			Timings:   engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: time.Minute, VMProgressInterval: time.Hour, FIPPollInterval: time.Millisecond},
		}, nil
	}, "test")
}

func TestSec_ProvisionPasswordMaskedOverHTTP(t *testing.T) {
	args := map[string]any{"name": "web", "confirmed": true}
	answer := func(t *testing.T, text string, isErr bool) map[string]any {
		t.Helper()
		var m map[string]any
		if isErr || json.Unmarshal([]byte(text), &m) != nil {
			t.Fatalf("answer (isErr %v): %s", isErr, text)
		}
		return m
	}

	cloud := &provisionCloud{}
	s := provisionServer(t, cloud)
	s.restrictForHTTP(false)
	text, isErr := call(s, "provision", args)
	res := answer(t, text, isErr)
	pw := cloud.password()
	if pw == "" || strings.Contains(text, pw) {
		t.Errorf("over HTTP the password leaks (or none was made, %q):\n%s", pw, text)
	}
	if res["password"] != redacted {
		t.Errorf("password = %v, want the mask", res["password"])
	}
	if got := redactedOf(t, text); len(got) != 1 || got[0] != "password" {
		t.Errorf("redacted = %v, want [password]", got)
	}
	if ssh, _ := res["ssh"].(string); !strings.HasPrefix(ssh, "ssh -i ") || !strings.HasSuffix(ssh, " user1@203.0.113.21") {
		t.Errorf("ssh is lost over HTTP: %v", res["ssh"])
	}

	for name, srv := range map[string]func() (*Server, *provisionCloud){
		"stdio": func() (*Server, *provisionCloud) {
			c := &provisionCloud{}
			return provisionServer(t, c), c
		},
		"--allow-secret-export": func() (*Server, *provisionCloud) {
			c := &provisionCloud{}
			s := provisionServer(t, c)
			s.restrictForHTTP(true)
			return s, c
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, c := srv()
			text, isErr := call(s, "provision", args)
			res := answer(t, text, isErr)
			if pw := c.password(); pw == "" || res["password"] != pw {
				t.Errorf("password = %v, the VM got %q", res["password"], pw)
			}
			if _, masked := res[redactedField]; masked {
				t.Errorf("masked: %s", text)
			}
		})
	}
}
