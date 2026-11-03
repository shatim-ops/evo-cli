# evo-stage: a long stage of evo on the VM (docker install, compose up),
# apart from the SSH session that starts it: the session can end — the
# budget of an MCP call, a lost connection, a closed client — and the stage
# goes on. A later session reads how it goes. One stage of a name at a time:
# a lock held by the stage's process for as long as it runs (flock, or a
# directory that names its owner where flock is missing); starts of a name
# take turns under a lock of their own. The files are the user's only: the
# script and the log may hold what the project prints.
#
#   evo-stage.sh start NAME INPUT SCRIPT_B64   prints started=true|false
#   evo-stage.sh status NAME                   prints state=, exit=, input=,
#                                              then --log-- and the log's tail
#   evo-stage.sh run NAME                      the stage itself (detached)
#
# state: none, running, done, failed, lost (running, but no process holds
# the lock: the VM rebooted, the process was killed).
set -u
umask 077
self=$0
cmd=$1
name=$2
d="$HOME/.evo/stage/$name"
mkdir -p "$d"

# starttime of process $1: field 22 of /proc/PID/stat, counted after the
# command name, which may hold spaces.
starttime() {
	sed 's/^.*) //' "/proc/$1/stat" 2>/dev/null | awk '{print $20}'
}

# owner_alive DIR: the process named in the lock directory DIR runs — the
# same pid, boot and start time, so a pid reused after a reboot is not
# taken for it.
owner_alive() {
	[ -f "$1/owner" ] || return 1
	read -r pid boot start <"$1/owner" || return 1
	[ "$boot" = "$(cat /proc/sys/kernel/random/boot_id)" ] || return 1
	[ -n "$start" ] && [ "$(starttime "$pid")" = "$start" ]
}

# held: a process runs the stage.
held() {
	if command -v flock >/dev/null 2>&1; then
		# The lock taken on a descriptor of this subshell: nothing else
		# to run, and it goes with the subshell.
		! (exec 8>>"$d/lock" && flock -n 8)
	else
		owner_alive "$d/lockdir"
	fi
}

# put FILE VALUE: the marker written aside and moved in — a status that
# reads it meanwhile sees the old value or the new, never an empty file.
put() {
	printf '%s\n' "$2" >"$d/$1.$$" && mv -f "$d/$1.$$" "$d/$1"
}

marker() {
	s=$(cat "$d/state" 2>/dev/null)
	[ -n "$s" ] || s=none
	echo "$s"
}

# state: the marker; running with no holder is lost. The stage writes its
# end before it lets the lock go, so a lock found free is checked against
# the marker read again: the stage may have ended in between.
state() {
	s=$(marker)
	if [ "$s" = running ] && ! held; then
		s=$(marker)
		[ "$s" != running ] || s=lost
	fi
	echo "$s"
}

# own DIR: names this process the owner of the lock directory DIR.
own() {
	echo "$$ $(cat /proc/sys/kernel/random/boot_id) $(starttime $$)" >"$1/owner"
}

# start_lock: the turn of this start — two starts at once would write the
# script and cut the log under each other. Up to 30 s; then exit 1.
start_lock() {
	if command -v flock >/dev/null 2>&1; then
		exec 7>>"$d/start.lock"
		flock -w 30 7 || exit 1
		return
	fi
	i=0
	until mkdir "$d/start.lockdir" 2>/dev/null; do
		# Left by a start that was killed.
		if ! owner_alive "$d/start.lockdir" && [ $i -gt 20 ]; then
			rm -rf "$d/start.lockdir"
		fi
		[ $i -lt 600 ] || exit 1
		sleep 0.05
		i=$((i + 1))
	done
	own "$d/start.lockdir"
	trap 'rm -rf "$d/start.lockdir"' EXIT
}

case $cmd in
status)
	echo "state=$(state)"
	echo "exit=$(cat "$d/exit" 2>/dev/null)"
	echo "input=$(cat "$d/input" 2>/dev/null)"
	echo "--log--"
	tail -n 20 "$d/log" 2>/dev/null || true
	;;
start)
	start_lock
	if [ "$(state)" = running ]; then
		echo started=false
		exit 0
	fi
	# The stage's lock is taken here, before the stage starts, and goes
	# with it: no moment where the stage is marked running and no process
	# holds it. flock: the descriptor the stage inherits (a stage that
	# ends lets it go a moment after its marker: wait for it). Without
	# flock: the directory, its owner the stage's process.
	if command -v flock >/dev/null 2>&1; then
		exec 9>>"$d/lock"
		flock -w 30 9 || exit 1
	else
		# A lock directory whose owner is gone is left from a killed stage.
		if [ -d "$d/lockdir" ] && ! owner_alive "$d/lockdir"; then
			rm -rf "$d/lockdir"
		fi
		i=0
		until mkdir "$d/lockdir" 2>/dev/null; do
			[ $i -lt 600 ] || exit 1
			sleep 0.05
			i=$((i + 1))
		done
	fi
	printf %s "$3" >"$d/input"
	printf %s "$4" | base64 -d >"$d/script"
	: >"$d/log"
	rm -f "$d/exit"
	put state running
	# The stage does not hold the turn of starts (descriptor 7).
	setsid nohup sh "$self" run "$name" </dev/null >/dev/null 2>&1 7>&- &
	if ! command -v flock >/dev/null 2>&1; then
		# setsid and nohup exec: $! is the stage's process. A stage
		# already over took the directory away: nothing to own.
		echo "$! $(cat /proc/sys/kernel/random/boot_id) $(starttime $!)" >"$d/lockdir/owner" 2>/dev/null || true
	fi
	echo started=true
	;;
run)
	# The lock is the start's, inherited (descriptor 9) or the directory.
	sh "$d/script" >>"$d/log" 2>&1
	ec=$?
	put exit "$ec"
	if [ "$ec" = 0 ]; then
		put state done
	else
		put state failed
	fi
	rm -rf "$d/lockdir"
	;;
esac
