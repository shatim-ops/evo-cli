// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run the emitted script as an ordinary Bash completion. The executable
// returns the real registry's offline answers for commands and records the
// exact arguments for cases whose values would require cloud credentials.
func TestBashCompletion_Readline(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash unavailable")
	}
	dir := t.TempDir()
	generate := func(file string, args ...string) {
		t.Helper()
		out, msg, code := execOffline(t, args...)
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, msg)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(out), 0600); err != nil {
			t.Fatal(err)
		}
	}
	generate("script", "completion", "bash", "--no-descriptions")
	generate("root", "__completeNoDesc", "v")
	generate("vm", "__completeNoDesc", "vm", "")
	test := `
touch_marker() { touch "$dir/executed"; }
if [[ -n $2 ]]; then source "$2" || exit 1; fi
source "$1/script" && source "$1/script" || exit 2
complete -p evo | grep -q __start_evo || exit 3
dir=$1
evo() {
    [[ $1 == __completeNoDesc ]] || return 1
    shift
    printf '%s\n' "$@" >"$dir/argv"
    case $fixture in
        root|vm) cat "$dir/$fixture" ;;
        list) printf 'list\n:4\n' ;;
        *) printf ':4\n' ;;
    esac
}
run() {
    COMP_CWORD=$1; COMP_LINE=$2; COMP_POINT=$3; shift 3
    COMP_WORDS=("$@")
    __start_evo
}
has() {
    local item
    for item in "${COMPREPLY[@]}"; do [[ $item == "$1" ]] && return 0; done
    return 1
}
fixture=root
run 1 'evo v' 5 evo v
for word in verify version vm vpc; do has "$word" || exit 4; done
fixture=vm
run 2 'evo vm ' 7 evo vm ''
has create && has list || exit 5
fixture=list
run 2 'evo vm list --json' 9 evo vm list --json
has list && [[ $(cat "$dir/argv") == $'vm\nli' ]] || exit 6
fixture=list
line='evo --name Москва list --json'
prefix='evo --name Москва li'
point=${#prefix}
if (( BASH_VERSINFO[0] == 3 )); then point=$(LC_ALL=C printf %s "$prefix" | wc -c); fi
run 3 "$line" "$point" evo --name Москва list --json
has list && [[ $(cat "$dir/argv") == $'--name\nМосква\nli' ]] || exit 10
fixture=args
line='evo --name "two words" --value=a:b'
# Readline represents the final nonempty word as the current field.
run 7 "$line" "${#line}" evo --name '"two words"' --value = a : b
[[ $(cat "$dir/argv") == $'--name\ntwo words\n--value=a:b' ]] || exit 7
line='evo --name $(touch_marker) a\ b'
run 3 "$line" "${#line}" evo --name '$(touch_marker)' 'a\ b'
[[ $(cat "$dir/argv") == $'--name\n$(touch_marker)\na b' ]] || exit 8
[[ ! -e "$dir/executed" ]] || exit 9
`
	loaders := []string{""}
	for _, candidate := range []string{"/usr/share/bash-completion/bash_completion", "/etc/bash_completion", "/opt/homebrew/etc/profile.d/bash_completion.sh", "/usr/local/etc/profile.d/bash_completion.sh"} {
		if _, err := os.Stat(candidate); err == nil {
			loaders = append(loaders, candidate)
			break
		}
	}
	for _, loader := range loaders {
		cmd := exec.Command(bash, "--noprofile", "--norc", "-c", test, "bash", dir, loader)
		cmd.Env = append(os.Environ(), "BASH_ENV=", "COMP_TYPE=9")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s loader %q: %v\n%s", bash, loader, err, strings.TrimSpace(string(out)))
		}
	}
}
