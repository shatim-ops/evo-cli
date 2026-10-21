// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Dogfood 1.2.1, feedback 3: after evo vm create the user could not log in.
// The hint was "ssh user1@IP" without evo's deploy key, the password evo
// generated for the image was lost, and nothing said that the key and the
// state live in the directory evo ran in. provision now finishes SSH access
// the way deploy_apply does, through the same code. All IDs and addresses
// are made up.

// keyImageFields are the metadata fields of an image that takes the key
// and a password (Ubuntu-like).
var keyImageFields = []provision.ImageMetadataField{
	{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"},
	{Name: "public_key", FieldType: "public_key", MandatoryGroup: " authentication"},
	{Name: "linux_password", FieldType: "linux_password", MandatoryGroup: "authentication", Sensitive: true},
	{Name: "hostname", FieldType: "hostname", MandatoryGroup: "hostname"},
}

// deployKeyCommand is the ssh command of a VM made with the deploy key of
// the project in dir.
func deployKeyCommand(dir, login, ip string) string {
	evo := filepath.Join(dir, ".evo")
	return "ssh -i " + filepath.Join(evo, "id_ed25519") + " -o UserKnownHostsFile=" + filepath.Join(evo, "known_hosts") +
		" -o StrictHostKeyChecking=accept-new " + login + "@" + ip
}

// seedKnownHosts writes the project's known_hosts: an entry of an earlier
// VM on ip (Cloud.ru reuses public IPs) and one of another host.
func seedKnownHosts(t *testing.T, dir, ip string) string {
	t.Helper()
	evo := filepath.Join(dir, ".evo")
	if err := os.MkdirAll(evo, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(evo, "known_hosts")
	if err := os.WriteFile(path, []byte(ip+" ssh-ed25519 AAAAold\n203.0.113.50 ssh-ed25519 AAAAother\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func answerOf(t *testing.T, text string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return m
}

func TestProvision_DeployKeySSHPasswordStateDir(t *testing.T) {
	dir := t.TempDir()
	knownHosts := seedKnownHosts(t, dir, "203.0.113.9")
	cloud := &progressCloud{states: []string{"active"}, fields: keyImageFields}
	text, isErr, seen := provisionRunIn(t, context.Background(), dir, cloud, true, output.ModePlain, time.Minute, nil)
	if isErr {
		t.Fatalf("answer: %s", text)
	}
	res := answerOf(t, text)
	ssh := deployKeyCommand(dir, "user1", "203.0.113.9")
	if res["ssh"] != ssh {
		t.Errorf("ssh = %v\nwant %s", res["ssh"], ssh)
	}
	if got := cloud.createReq.ImageMetadata["public_key"]; got != "ssh-ed25519 AAAA test" {
		t.Errorf("the VM did not get the deploy key: %q", got)
	}
	// The password is the one the VM got, once: only in its field.
	pw := cloud.createReq.ImageMetadata["linux_password"]
	if pw == "" || res["password"] != pw {
		t.Errorf("password = %v, the VM got %q", res["password"], pw)
	}
	if n := strings.Count(text, pw); n != 1 {
		t.Errorf("the password is %d times in the answer, want once:\n%s", n, text)
	}
	if res["state_dir"] != filepath.Join(dir, ".evo") {
		t.Errorf("state_dir = %v, want %s", res["state_dir"], filepath.Join(dir, ".evo"))
	}
	want := "VM vm-made is active at 203.0.113.9. SSH: " + ssh +
		" The VM password is in this answer only (password): give it to the user now, evo keeps it nowhere."
	if res["message"] != want {
		t.Errorf("message:\n got %v\nwant %s", res["message"], want)
	}
	// The earlier VM's host key is dropped, the other host's kept: the
	// first ssh records the new key (accept-new).
	if b, _ := os.ReadFile(knownHosts); string(b) != "203.0.113.50 ssh-ed25519 AAAAother\n" {
		t.Errorf("known_hosts:\n%s", b)
	}
	// The summary on stderr gives the same command.
	if !strings.Contains(seen, ". SSH: "+ssh+"\n") {
		t.Errorf("the summary has not the ssh command %q:\n%s", ssh, seen)
	}
	if strings.Contains(seen, pw) {
		t.Errorf("the password in the progress lines:\n%s", seen)
	}
}

// Values the caller gave are not echoed back: the password is theirs, and
// with their own key the deploy key's command would not log in.
func TestProvision_CallerValuesNotEchoed(t *testing.T) {
	cloud := &progressCloud{states: []string{"active"}, fields: keyImageFields}
	text, isErr, seen, dir := provisionRun(t, context.Background(), cloud, true, output.ModePlain, time.Minute,
		map[string]any{"image_metadata": map[string]any{"linux_password": "Given-Pa55word!", "public_key": "ssh-ed25519 AAAAuser me"}})
	if isErr {
		t.Fatalf("answer: %s", text)
	}
	res := answerOf(t, text)
	if strings.Contains(text, "Given-Pa55word!") {
		t.Errorf("the caller's password is echoed:\n%s", text)
	}
	if _, ok := res["password"]; ok {
		t.Errorf("password in the answer: %v", res["password"])
	}
	if _, ok := res["ssh"]; ok {
		t.Errorf("ssh with the deploy key for a VM without it: %v", res["ssh"])
	}
	if res["message"] != "VM vm-made is active at 203.0.113.9. SSH: ssh user1@203.0.113.9" {
		t.Errorf("message: %v", res["message"])
	}
	if !strings.Contains(seen, ". SSH: ssh user1@203.0.113.9\n") {
		t.Errorf("summary:\n%s", seen)
	}
	if res["state_dir"] != filepath.Join(dir, ".evo") {
		t.Errorf("state_dir = %v", res["state_dir"])
	}
}

// A private VM keeps its message; the password evo made comes back all the
// same.
func TestProvision_PrivateVMPassword(t *testing.T) {
	cloud := &progressCloud{states: []string{"active"}, fields: keyImageFields}
	text, isErr, _, _ := provisionRun(t, context.Background(), cloud, false, output.ModePlain, time.Minute,
		map[string]any{"no_public_ip": true})
	if isErr {
		t.Fatalf("answer: %s", text)
	}
	res := answerOf(t, text)
	if _, ok := res["ssh"]; ok {
		t.Errorf("ssh for a private VM: %v", res["ssh"])
	}
	pw := cloud.createReq.ImageMetadata["linux_password"]
	if pw == "" || res["password"] != pw {
		t.Errorf("password = %v, the VM got %q", res["password"], pw)
	}
	want := "VM vm-made is active (private — no public IP, interface type 'regular'). Usable as a load-balancer target; reach it only from inside the VPC." +
		" The VM password is in this answer only (password): give it to the user now, evo keeps it nowhere."
	if res["message"] != want {
		t.Errorf("message:\n got %v\nwant %s", res["message"], want)
	}
}

// A VM not active in time may still become one: its password goes out in
// the timeout answer, as deploy_apply's does.
func TestProvision_TimeoutAnswerHasPassword(t *testing.T) {
	cloud := &progressCloud{states: []string{"creating"}, fields: keyImageFields}
	text, isErr, _, _ := provisionRun(t, context.Background(), cloud, false, output.ModePlain, 30*time.Millisecond, nil)
	if !isErr {
		t.Fatalf("not an error: %s", text)
	}
	res := answerOf(t, text)
	pw := cloud.createReq.ImageMetadata["linux_password"]
	if res["status"] != "timeout" || pw == "" || res["password"] != pw {
		t.Errorf("status %v, password %v (the VM got %q)", res["status"], res["password"], pw)
	}
	if n := strings.Count(text, pw); n != 1 {
		t.Errorf("the password is %d times in the answer, want once:\n%s", n, text)
	}
}

// deploy_apply's SSH answer is the one it gave before provision shared its
// code: the command, the note and the project's known_hosts.
func TestDeployApply_DeployKeySSHUnchanged(t *testing.T) {
	cloud := &faultCloud{}
	r, dir := faultToolsRegistry(t, cloud)
	prev := deployplan.SSHPortWait
	deployplan.SSHPortWait = time.Millisecond
	t.Cleanup(func() { deployplan.SSHPortWait = prev })
	plan := faultPlan()
	plan.PublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test" // the engine's deploy key
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	knownHosts := seedKnownHosts(t, dir, "203.0.113.12")
	text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": plan.PlanID, "confirmed": true})
	if isErr {
		t.Fatalf("answer: %s", text)
	}
	res := answerOf(t, text)
	if res["public_ip"] != "203.0.113.12" {
		t.Fatalf("public_ip: %v", res["public_ip"])
	}
	if want := deployKeyCommand(dir, "user1", "203.0.113.12"); res["ssh"] != want {
		t.Errorf("ssh:\n got %v\nwant %s", res["ssh"], want)
	}
	want := "public IP attached (created a new floating IP); VM is up but SSH port 22 was not reachable yet — wait ~1 min and retry the same ssh command. " +
		"The VM is still booting; do NOT blame security groups, do NOT recreate the VM.; Connect with the ssh command above (agent deploy key). " +
		"No .evo/user_authorized_keys configured — to give the user SSH access, append their public key to ~/.ssh/authorized_keys on the VM after install."
	if res["note"] != want {
		t.Errorf("note:\n got %v\nwant %s", res["note"], want)
	}
	for _, k := range []string{"state_dir", "message"} {
		if _, ok := res[k]; ok {
			t.Errorf("deploy_apply got %s: %v", k, res[k])
		}
	}
	if b, _ := os.ReadFile(knownHosts); string(b) != "203.0.113.50 ssh-ed25519 AAAAother\n" {
		t.Errorf("known_hosts:\n%s", b)
	}
}

// known_hosts that cannot be updated: the answer says so, in the same words
// for both tools, and the VM is not failed for it.
func TestDeployKeySSH_KnownHostsNotWritable(t *testing.T) {
	blockKnownHosts := func(t *testing.T, dir string) string {
		t.Helper()
		path := filepath.Join(dir, ".evo", "known_hosts")
		if err := os.MkdirAll(path, 0o700); err != nil { // a directory: not readable as a file
			t.Fatal(err)
		}
		return path
	}
	warned := func(t *testing.T, text, path, ip string) {
		t.Helper()
		head := " WARNING: could not update " + path + " ("
		tail := "); if ssh reports a changed host key, remove the line for " + ip + " from it."
		i := strings.Index(text, head)
		if i < 0 || !strings.Contains(text[i:], tail) {
			t.Errorf("no warning %q…%q in:\n%s", head, tail, text)
		}
	}

	t.Run("provision", func(t *testing.T) {
		dir := t.TempDir()
		path := blockKnownHosts(t, dir)
		cloud := &progressCloud{states: []string{"active"}, fields: keyImageFields}
		text, isErr, _ := provisionRunIn(t, context.Background(), dir, cloud, false, output.ModePlain, time.Minute, nil)
		if isErr {
			t.Fatalf("answer: %s", text)
		}
		msg, _ := answerOf(t, text)["message"].(string)
		warned(t, msg, path, "203.0.113.9")
	})
	t.Run("deploy_apply", func(t *testing.T) {
		cloud := &faultCloud{}
		r, dir := faultToolsRegistry(t, cloud)
		prev := deployplan.SSHPortWait
		deployplan.SSHPortWait = time.Millisecond
		t.Cleanup(func() { deployplan.SSHPortWait = prev })
		plan := faultPlan()
		plan.PublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test"
		if err := deployplan.Save(dir, plan); err != nil {
			t.Fatal(err)
		}
		path := blockKnownHosts(t, dir)
		text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": plan.PlanID, "confirmed": true})
		if isErr {
			t.Fatalf("answer: %s", text)
		}
		note, _ := answerOf(t, text)["note"].(string)
		warned(t, note, path, "203.0.113.12")
	})
}
