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

state() {
	s=$(cat "$d/state" 2>/dev/null || echo none)
	if [ "$s" = running ] && ! held; then
		s=lost
	fi
	echo "$s"
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
	# A lock directory whose owner is gone is left from a killed stage.
	if [ -d "$d/lockdir" ] && ! owner_alive "$d/lockdir"; then
		rm -rf "$d/lockdir"
	fi
	printf %s "$3" >"$d/input"
	printf %s "$4" | base64 -d >"$d/script"
	: >"$d/log"
	rm -f "$d/exit"
	echo running >"$d/state"
	# The stage does not hold the turn of starts (descriptor 7).
	setsid nohup sh "$self" run "$name" </dev/null >/dev/null 2>&1 7>&- &
	# Started once its process holds the lock, or ended already.
	i=0
	while [ $i -lt 100 ] && ! held && [ "$(cat "$d/state")" = running ]; do
		sleep 0.05
		i=$((i + 1))
	done
	echo started=true
	;;
run)
	if command -v flock >/dev/null 2>&1; then
		exec 9>>"$d/lock"
		# A check of the state (held) takes the lock for an instant:
		# wait it out rather than give up.
		flock -w 30 9 || exit 0
	else
		mkdir "$d/lockdir" 2>/dev/null || exit 0
		own "$d/lockdir"
	fi
	sh "$d/script" >>"$d/log" 2>&1
	ec=$?
	echo "$ec" >"$d/exit"
	if [ "$ec" = 0 ]; then
		echo done >"$d/state"
	else
		echo failed >"$d/state"
	fi
	rm -rf "$d/lockdir"
	;;
esac
