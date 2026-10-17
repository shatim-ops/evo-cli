#!/bin/sh
# Установка evo из GitHub Releases (Linux, macOS). До v1.1 рядом ставится
# alias cloudru-vm (тот же бинарь под старым именем): скрипты и настройки
# MCP со старым именем продолжают работать и получают ту же версию.
#
#   curl -fsSL https://raw.githubusercontent.com/shatim-ops/evo-cli/main/install.sh | sh
#
# Переменные окружения:
#   CLOUDRU_VM_VERSION  тег релиза, например v1.0.0 (по умолчанию —
#                       последний стабильный; pre-release надо назвать явно)
#   INSTALL_DIR         куда положить бинари (по умолчанию /usr/local/bin
#                       для root, иначе ~/.local/bin)
#   CLOUDRU_VM_BASE_URL откуда качать вместо GitHub (зеркало, проверка
#                       скрипта); ожидается <base>/<тег>/<файл>
#
# Скрипт сверяет sha256 архива с checksums.txt релиза. Если в PATH есть
# cosign, он же проверяет подпись checksums.txt (keyless, identity —
# release.yml этого репозитория). Windows: см. README, раздел «Установка».
set -eu

REPO="shatim-ops/evo-cli"
# Имя репозитория до 15.10.2026: им подписаны v1.0.0-rc.1 и rc.2.
LEGACY_REPO="shatim-ops/cloudru-vm-cli"
BINARY="evo"
# Убрать в v1.1 вместе с alias в .goreleaser.yaml.
LEGACY_ALIAS="cloudru-vm"

say() { printf '%s\n' "$*" >&2; }
die() { say "install.sh: $*"; exit 1; }

need() {
	command -v "$1" >/dev/null 2>&1 || die "нужна утилита $1"
}

# Без cosign целостность держится на TLS: checksums.txt приходит оттуда же,
# откуда архив. Поэтому для https-адреса curl не идёт ни на что, кроме
# https, и в том числе по редиректам (GitHub отдаёт файлы релиза редиректом
# на другой хост): иначе «https → http» отдал бы архив и суммы тому, кто
# сидит в сети.
download() { # url file
	if command -v curl >/dev/null 2>&1; then
		case "$1" in
		https://*) curl --proto '=https' --tlsv1.2 -fsSL -o "$2" "$1" ;;
		*) curl -fsSL -o "$2" "$1" ;;
		esac
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "нужен curl или wget"
	fi
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) die "ОС $(uname -s) не поддерживается скриптом; для Windows — zip из релиза" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "архитектура $(uname -m) не поддерживается (есть amd64, arm64)" ;;
	esac
}

# Последний стабильный тег — из редиректа /releases/latest, без API и его
# лимитов. Pre-release (rc) туда не попадает.
latest_tag() {
	need curl
	url=$(curl --proto '=https' --tlsv1.2 -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
		die "не удалось узнать последний релиз; задайте CLOUDRU_VM_VERSION"
	tag=${url##*/}
	case "$tag" in
	v*) echo "$tag" ;;
	*) die "стабильного релиза пока нет; задайте CLOUDRU_VM_VERSION, например v1.0.0" ;;
	esac
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "нужен sha256sum или shasum"
	fi
}

main() {
	need uname
	need tar
	os=$(detect_os)
	arch=$(detect_arch)
	tag=${CLOUDRU_VM_VERSION:-}
	[ -n "$tag" ] || tag=$(latest_tag)
	case "$tag" in v*) ;; *) tag="v$tag" ;; esac
	version=${tag#v}

	base=${CLOUDRU_VM_BASE_URL:-https://github.com/$REPO/releases/download}
	case "$base" in
	https://*) ;;
	*) say "внимание: $base — не https; без cosign подлинность архива не проверяется ничем" ;;
	esac

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	download "$base/$tag/checksums.txt" "$tmp/checksums.txt" || die "не удалось скачать $base/$tag/checksums.txt"

	if command -v cosign >/dev/null 2>&1; then
		download "$base/$tag/checksums.txt.sigstore.json" "$tmp/checksums.txt.sigstore.json" ||
			die "не удалось скачать подпись checksums.txt.sigstore.json"
		# v1.0.0-rc.1 и rc.2 подписаны до переименования репозитория:
		# identity в сертификате — старое имя.
		signer=$REPO
		case "$tag" in
		v1.0.0-rc.1 | v1.0.0-rc.2) signer=$LEGACY_REPO ;;
		esac
		# Релизы подписывает cosign v3: бандл в формате sigstore v0.3. cosign
		# v2 читает его только с --new-bundle-format (есть с v2.4), без
		# флага — «bundle does not contain cert». Сначала без флага (v3 и
		# старые релизы), затем с ним, если cosign его знает.
		verify() {
			cosign verify-blob "$@" \
				--bundle "$tmp/checksums.txt.sigstore.json" \
				--certificate-identity "https://github.com/$signer/.github/workflows/release.yml@refs/tags/$tag" \
				--certificate-oidc-issuer https://token.actions.githubusercontent.com \
				"$tmp/checksums.txt" >"$tmp/cosign.log" 2>&1
		}
		if ! verify; then
			if cosign verify-blob --help 2>&1 | grep -q -- --new-bundle-format; then
				verify --new-bundle-format
			else
				false
			fi || {
				sed 's/^/  cosign: /' "$tmp/cosign.log" >&2
				die "подпись checksums.txt не прошла проверку cosign (нужен cosign v2.4 или новее)"
			}
		fi
		say "Подпись checksums.txt: OK (cosign)"
	else
		say "cosign не найден: подпись не проверена, только sha256"
	fi

	# Релизы до переименования (v1.0.0-rc.1) выпущены под старым именем:
	# архив cloudru-vm_<версия>_… с одним бинарём cloudru-vm. Какое имя у
	# этого релиза, говорит подписанный checksums.txt.
	bins="$BINARY $LEGACY_ALIAS"
	archive="${BINARY}_${version}_${os}_${arch}.tar.gz"
	want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
	if [ -z "$want" ]; then
		legacy_archive="${LEGACY_ALIAS}_${version}_${os}_${arch}.tar.gz"
		want=$(awk -v f="$legacy_archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
		[ -n "$want" ] || die "$archive нет в checksums.txt"
		archive=$legacy_archive
		bins=$LEGACY_ALIAS
		say "$tag выпущен до переименования в $BINARY: ставлю бинарь $LEGACY_ALIAS"
	fi

	say "Скачиваю $archive ($tag)"
	download "$base/$tag/$archive" "$tmp/$archive" || die "не удалось скачать $base/$tag/$archive"
	got=$(sha256_of "$tmp/$archive")
	[ "$want" = "$got" ] || die "sha256 не совпал: ожидался $want, получен $got"
	say "sha256: OK"

	# shellcheck disable=SC2086 # bins — список имён без пробелов
	tar -xzf "$tmp/$archive" -C "$tmp" $bins

	dir=${INSTALL_DIR:-}
	if [ -z "$dir" ]; then
		if [ "$(id -u)" -eq 0 ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
	fi
	mkdir -p "$dir"
	# Через временное имя: запущенный старый бинарь не ломается на середине.
	for b in $bins; do
		cp "$tmp/$b" "$dir/.$b.new"
		chmod 0755 "$dir/.$b.new"
		mv -f "$dir/.$b.new" "$dir/$b"
	done

	main_bin=${bins%% *}
	if [ "$main_bin" = "$BINARY" ]; then
		say "Установлено: $dir/$BINARY (и alias $dir/$LEGACY_ALIAS до v1.1)"
	else
		say "Установлено: $dir/$LEGACY_ALIAS"
	fi
	case ":$PATH:" in
	*":$dir:"*) ;;
	*) say "Каталога $dir нет в PATH. Добавьте в профиль оболочки: export PATH=\"$dir:\$PATH\"" ;;
	esac
	"$dir/$main_bin" version >&2 || true
}

main "$@"
