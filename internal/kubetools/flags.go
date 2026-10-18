// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package kubetools

import (
	"fmt"
	"strings"
)

// valueShorthands are the one-letter kubectl flags that always take a
// value: in a cluster like -Aoyaml the letters after them are the value.
// Any other letter may be a boolean (-A, -w, -i, -t, -R, -q, -p, -f), so the
// letter after it is read as another flag.
const valueShorthands = "nlLcosvk"

// Flag is one flag of a kubectl argument: "--name=val", "-n val",
// or one letter of a shorthand cluster ("-it", "-Aoyaml").
type Flag struct {
	Name, Val string
	HasVal    bool
}

// ParseFlags splits a flag argument the way kubectl's flag parser does.
// Names are as kubectl sees them after turning "_" into "-" (kubectl
// accepts --client_key for --client-key).
func ParseFlags(arg string) []Flag {
	if strings.HasPrefix(arg, "--") {
		name, val, hasVal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		return []Flag{{strings.ReplaceAll(name, "_", "-"), val, hasVal}}
	}
	var out []Flag
	s := arg[1:]
	for j := 0; j < len(s); j++ {
		c := s[j]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			break // "=value" of a boolean, or not a flag: kubectl rejects it
		}
		if strings.IndexByte(valueShorthands, c) >= 0 {
			rest := s[j+1:]
			out = append(out, Flag{string(c), strings.TrimPrefix(rest, "="), rest != ""})
			break
		}
		out = append(out, Flag{Name: string(c)})
	}
	return out
}

// connectionFlags replace the cluster or the credentials of the kubeconfig
// evo writes: with them "get pods --server=https://elsewhere" sends the
// cluster's token to another server, and --kubeconfig reads one from the
// host. evo's kubeconfig always names its cluster, so kubectl through evo
// never needs them (security review S1-app 2).
var connectionFlags = map[string]bool{
	"kubeconfig": true, "server": true, "s": true, "token": true, "username": true,
	"password": true, "client-certificate": true, "client-key": true,
	"certificate-authority": true, "insecure-skip-tls-verify": true,
	"tls-server-name": true,
}

// CheckConnectionFlags refuses kubectl arguments that point the call at
// another cluster or credentials than evo's kubeconfig, and --kuberc: a
// preferences file may set flag defaults (release audit, R-05). The words
// after "--" are the command run in a pod (exec, debug, run), not kubectl
// flags.
func CheckConnectionFlags(fields []string) error {
	for _, f := range fields {
		if f == "--" {
			return nil
		}
		if !strings.HasPrefix(f, "-") || f == "-" {
			continue
		}
		for _, fl := range ParseFlags(f) {
			if fl.Name == "kuberc" {
				return fmt.Errorf("kubectl flag %s is not allowed: evo runs kubectl with KUBERC=off, so that a preferences file (aliases, flag defaults) cannot change its commands", f)
			}
			if connectionFlags[fl.Name] {
				return fmt.Errorf("kubectl flag %s is not allowed: it replaces the cluster or credentials of the kubeconfig evo writes (the cluster's token would go elsewhere); the cluster is chosen by cluster_id", f)
			}
		}
	}
	return nil
}
