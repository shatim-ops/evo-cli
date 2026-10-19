#!/bin/sh
# Prepares a fresh VM for `docker compose up`. Runs as root (via sudo) over
# SSH, so it does not depend on the compute API accepting user-data: Cloud.ru
# renders cloud-init only from the image's own template. Idempotent: on a VM
# that already has Docker it only fixes the group and the app directory.
# $1 is the login user that runs docker compose.
#
# Ubuntu and Debian get Docker from Docker's apt repository, signed by the
# key embedded below (fingerprint 9DC8 5822 9FC7 DD38 854A E2D8 8D81 803C
# 0EBF CD88, checked by install_test.go). The image's apt mirror often does
# not answer from a new VM (cloud-e2e 16-18.10.2026, register OPS-2: "W:
# Failed to fetch https://repo.mng.sbercloud..."): then this install alone
# goes to the distribution's official mirror over HTTPS. The image's own apt
# sources are not changed, and apt checks the signatures of every source.
# Other distributions get Docker from get.docker.com, as before.
set -eu
user="$1"
# EVO_ROOT stands for / in install_test.go; sudo does not pass it, so on a
# VM it is always empty.
root="${EVO_ROOT:-}"

evo_dir="$root/var/lib/evo"
apt_log="$evo_dir/apt.log"
fallback_marker="$evo_dir/apt-fallback"
fallback_dir="$evo_dir/apt"
docker_key="$root/etc/apt/keyrings/docker.asc"
docker_list="$root/etc/apt/sources.list.d/docker.list"
# The image template's bootcmd "configure-apt-mirrors" writes this on success.
mirror_marker="$root/var/lib/cloud/instance/sem/configure-apt-mirrors"
docker_pkgs="docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin"

# Short network timeouts: a mirror that does not answer fails in seconds,
# not after apt's two minutes per connection. Each apt-get also has a time
# limit; evo runs the whole script again until its own deadline.
apt_net="-o Acquire::Retries=2 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 -o DPkg::Lock::Timeout=120"
# A conffile the image changed keeps the image's version, without a prompt.
dpkg_opts="-o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold"
apt_src="" # empty: the image's own sources; set by use_fallback

say() { echo "evo: $*"; }

# apt_get LIMIT ARGS... runs apt-get with the timeouts and the current
# sources for at most LIMIT seconds; the output goes to $apt_log.
apt_get() {
	limit="$1"
	shift
	apt_rc=0
	# shellcheck disable=SC2086 # option lists: words without spaces
	DEBIAN_FRONTEND=noninteractive timeout "$limit" apt-get -q $apt_net $apt_src "$@" >"$apt_log" 2>&1 || apt_rc=$?
	if [ "$apt_rc" = 124 ]; then
		echo "E: apt-get $1 did not finish in ${limit}s (timed out)" >>"$apt_log"
	fi
	return "$apt_rc"
}

# apt_update LIMIT: "Failed to fetch" leaves apt-get update with exit code 0.
apt_update() {
	apt_get "$1" update || return 1
	! grep -qE '^(W|E): (Failed to fetch|Some index files failed)' "$apt_log"
}

apt_errors() { grep -E '^(W|E):' "$apt_log" | head -5 || true; }

# docker_repo_failed: an apt error names Docker's repository. Another
# Ubuntu or Debian mirror would not help.
docker_repo_failed() {
	grep -E '^(W|E):' "$apt_log" | grep -q 'download\.docker\.com'
}

# mirror_failed: an apt error about a source other than Docker's repository
# (a lock held by another apt is not one) — the image's mirror is down or
# broken, and the official mirror may help.
mirror_failed() {
	grep -E '^(W|E):' "$apt_log" | grep -v 'download\.docker\.com' |
		grep -qE 'Failed to fetch|Some index files failed|GPG error|is not signed|Release file|timed out'
}

first_file() {
	for f in "$@"; do
		if [ -f "$f" ]; then
			echo "$f"
			return 0
		fi
	done
	true
}

# use_fallback points apt at the official mirror of the distribution and
# Docker's repository, for the rest of this install and for evo's next
# attempts (the marker). Signatures are checked against the distribution's
# keyring, as for the image's sources.
use_fallback() {
	case "$os_id" in
	ubuntu)
		case "$arch" in
		amd64 | i386)
			base=https://archive.ubuntu.com/ubuntu
			security=https://security.ubuntu.com/ubuntu
			;;
		*)
			base=https://ports.ubuntu.com/ubuntu-ports
			security=$base
			;;
		esac
		components="main universe"
		keyring=$(first_file "$root/usr/share/keyrings/ubuntu-archive-keyring.gpg" "$root/usr/share/keyrings/ubuntu-archive-keyring.pgp")
		;;
	debian)
		base=https://deb.debian.org/debian
		security=https://deb.debian.org/debian-security
		components=main
		keyring=$(first_file "$root/usr/share/keyrings/debian-archive-keyring.gpg" "$root/usr/share/keyrings/debian-archive-keyring.pgp")
		;;
	esac
	# Without the keyring file apt checks against its trusted keys, as it
	# does for the image's sources: never an unsigned source.
	signed=""
	if [ -n "$keyring" ]; then
		signed="[signed-by=$keyring] "
	fi
	mkdir -p "$fallback_dir/sources.list.d"
	{
		echo "deb $signed$base $codename $components"
		echo "deb $signed$base $codename-updates $components"
		echo "deb $signed$security $codename-security $components"
	} >"$fallback_dir/sources.list"
	cp "$docker_list" "$fallback_dir/sources.list.d/docker.list"
	apt_src="-o Dir::Etc::SourceList=$fallback_dir/sources.list -o Dir::Etc::SourceParts=$fallback_dir/sources.list.d"
	: >"$fallback_marker"
	say "switching apt to the official mirror for this install: $base (the image's apt sources stay as they are)"
}

# fallback_failed and apt_failed end the script with apt's errors.
fallback_failed() {
	if docker_repo_failed && ! mirror_failed; then
		apt_failed "$1"
	fi
	say "the official mirror failed too ($base):"
	apt_errors
	exit 1
}

apt_failed() {
	if docker_repo_failed; then
		if grep -E '^(W|E):' "$apt_log" | grep 'download\.docker\.com' | grep -qE 'GPG error|is not signed'; then
			say "apt failed on Docker's repository (download.docker.com): its signature does not verify"
		else
			say "apt failed on Docker's repository (download.docker.com): no answer"
		fi
	else
		say "apt-get $1 failed:"
	fi
	apt_errors
	exit 1
}

# shellcheck disable=SC2086 # a list of package names
docker_install() { apt_get 600 install -y $dpkg_opts $docker_pkgs; }

# install_docker_apt installs Docker from Docker's apt repository with the
# image's apt sources, or with the official mirror when those fail.
install_docker_apt() {
	# shellcheck source=/dev/null
	codename=$(. "$root/etc/os-release" && echo "${VERSION_CODENAME:-${UBUNTU_CODENAME:-}}")
	if [ -z "$codename" ]; then
		say "no VERSION_CODENAME in /etc/os-release: cannot pick Docker's repository"
		return 1
	fi
	arch=$(dpkg --print-architecture)

	# The same paths as get.docker.com, so a repository it set up earlier is
	# replaced, not doubled with another signed-by.
	install -d -m 0755 "$root/etc/apt/keyrings" "$root/etc/apt/sources.list.d"
	cat >"$docker_key" <<'EOF'
@DOCKER_KEY@
EOF
	chmod 0644 "$docker_key"
	echo "deb [arch=$arch signed-by=$docker_key] https://download.docker.com/linux/$os_id $codename stable" >"$docker_list"
	# An earlier attempt cut by the deadline may have stopped dpkg midway.
	DEBIAN_FRONTEND=noninteractive dpkg --configure -a >/dev/null 2>&1 || true

	# An earlier attempt found the image's mirror down: do not wait on it again.
	if [ -f "$fallback_marker" ]; then
		use_fallback
	fi

	if ! apt_update 150; then
		if [ -n "$apt_src" ]; then
			fallback_failed update
		fi
		if ! mirror_failed; then
			apt_failed update
		fi
		say "apt-get update failed with the image's apt sources:"
		apt_errors
		# Cloud.ru images switch apt to the platform mirror at first boot
		# only after checking that the mirror answers. A VM can get its
		# network late, the check fails and leaves the image's stale
		# sources: run the platform's own step again first.
		updated=""
		if [ ! -f "$mirror_marker" ] && command -v cloud-init >/dev/null 2>&1; then
			say "re-running the image's apt mirror setup (cloud-init bootcmd)"
			timeout 180 cloud-init single --name bootcmd --frequency always >/dev/null 2>&1 || true
			if apt_update 150; then
				updated=1
			fi
		fi
		if [ -z "$updated" ]; then
			use_fallback
			apt_update 180 || fallback_failed update
		fi
	fi

	if ! docker_install; then
		if [ -n "$apt_src" ]; then
			fallback_failed install
		fi
		if ! mirror_failed; then
			apt_failed install
		fi
		say "apt-get install failed with the image's apt sources:"
		apt_errors
		use_fallback
		apt_update 180 || fallback_failed update
		docker_install || fallback_failed install
	fi
}

# First-boot cloud-init holds the apt lock (the platform template runs
# package_update). Its exit code is not trusted: warnings give 2.
if command -v cloud-init >/dev/null 2>&1; then
	cloud-init status --wait >/dev/null 2>&1 || true
fi

if ! docker compose version >/dev/null 2>&1; then
	mkdir -p "$evo_dir" "$root/etc/docker"
	if [ ! -f "$root/etc/docker/daemon.json" ]; then
		cat >"$root/etc/docker/daemon.json" <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": {"max-size": "10m", "max-file": "3"}
}
EOF
	fi
	# shellcheck source=/dev/null
	os_id=$(. "$root/etc/os-release" && echo "${ID:-}")
	case "$os_id" in
	ubuntu | debian)
		install_docker_apt
		;;
	*)
		if ! command -v curl >/dev/null 2>&1; then
			say "curl is missing: cannot run get.docker.com on $os_id"
			exit 1
		fi
		curl -fsSL https://get.docker.com | sh
		;;
	esac
fi

systemctl enable --now docker >/dev/null 2>&1 || true
usermod -aG docker "$user"
install -d -o "$user" "$root/opt/app"
docker compose version
