#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Exercise both cleanup steps against a fake CA/AR service without credentials.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source-path=SCRIPTDIR source=cloud_live_ca_receipts.sh
. "$here/cloud_live_ca_receipts.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
PROJECT=test-project RUN=r7a1 RUNNER_TEMP=$tmp
mask() { :; }
sha() { printf %s "$1" | sha256sum | cut -c1-64; }
ok() { printf 'OK\t%s\t%s\n' "$1" "${2:-}" >>"$tmp/events"; }
bad() { printf 'BAD\t%s\t%s\n' "$1" "${2:-}" >>"$tmp/events"; }
row() { printf 'ROW\t%s\t%s\t%s\n' "$1" "$2" "${3:-}" >>"$tmp/events"; }
cov() { :; }
iam_fresh() { [ ! -f "$tmp/iam-fail" ]; }
poll() { shift 2; "$@"; }
evo() {
	local name id kind
	[[ $1 = --project-id && $2 = "$PROJECT" && $3 = tools && $4 = call ]] || return 1
	kind=${5%_*}; name=$(ca_run_name "$kind")
	id=$(cat "$tmp/$kind.current") || return 1
	case "$5" in
	ca_list|ar_list)
		local field=containers
		[ "$kind" = ca ] || field=registries
		if [[ $kind == ar && -f $tmp/ar-list-failed ]]; then return 1; fi
		if [[ $kind == ca && -f $tmp/ca-list-still ]]; then
			jq -nc --arg f "$field" --arg n "$name" --arg id "$id" '{($f):[{name:$n,id:$id}]}'
			return 0
		fi
		jq -nc --arg f "$field" --arg n "$name" --arg id "$id" '{($f):([{name:$n,id:$id}]|map(select(.id!="gone")))}' ;;
	ca_get|ar_get)
		[[ $id != gone ]] || return 1
		local m=
		if [[ $kind = ca ]]; then m=$(cat "$tmp/ca-description"); fi
		jq -nc --arg n "$name" --arg id "$id" --arg p "$PROJECT" --arg m "$m" '{name:$n,id:$id,projectId:$p,description:$m}' ;;
	ar_operation)
		[[ $6 == --arg && $7 == operation_id=delete-ar-op && $8 == --json ]] || return 1
		cat "$tmp/ar-op-answer" ;;
	*) return 1 ;;
	esac
}
api() {
	local kind=ca
	[[ $1 = GET ]] || return 1
	case "$2" in
	"https://ar.api.cloud.ru/v1/registries/our-ar?projectId=$PROJECT") kind="ar" ;;
	"https://containers.api.cloud.ru/v2/containers/$(ca_run_name ca)?projectId=$PROJECT") ;;
	*) return 1 ;;
	esac
	if [[ $kind == ca && -f $tmp/ca-mode ]]; then
		case "$(cat "$tmp/ca-mode")" in
		200-deleting)
			code=200
			echo '{"id":"our-ca","status":"DELETING"}' >"$tmp/api.json" ;;
		404-gateway)
			code=404
			echo '{"message":"Route not found"}' >"$tmp/api.json" ;;
		403-forbidden)
			code=403
			echo '{"code":7,"message":"Forbidden"}' >"$tmp/api.json" ;;
		499-named)
			code=499
			jq -nc --arg n "$(ca_run_name ca)" '{message:("Serverless with name \""+$n+"\" is not found")}' >"$tmp/api.json" ;;
		404-named-str5)
			code=404
			jq -nc --arg n "$(ca_run_name ca)" '{code:"5",message:("container \""+$n+"\" not found")}' >"$tmp/api.json" ;;
		499-other-app)
			code=499
			echo '{"message":"Serverless with name \"other-app\" is not found"}' >"$tmp/api.json" ;;
		*) return 1 ;;
		esac
		return 0
	fi
	[[ $(cat "$tmp/$kind.current") = gone ]] || { code=200; return 0; }
	code=404
	if [[ $kind == ar && -f $tmp/ar-forbidden ]]; then code=403; echo '{"code":7,"message":"Forbidden"}' >"$tmp/api.json"
	elif [ -f "$tmp/gateway" ]; then echo '{"message":"Route not found"}' >"$tmp/api.json"
	elif [ "$kind" = ca ]; then echo '{"code":5,"message":"container not found"}' >"$tmp/api.json"
	else echo '{"code":5,"message":"registry not found"}' >"$tmp/api.json"; fi
}
tc() {
	rc=0
	case "$*" in
	"ca_delete --arg name=$(ca_run_name ca) --arg wait=true")
		if [ -f "$tmp/ca-tc-mode" ]; then
			case "$(cat "$tmp/ca-tc-mode")" in
			deleting)
				rc=0; reason=-; why=
				jq -nc --arg n "$(ca_run_name ca)" '{name:$n,status:"deleting"}' >"$tmp/r.json" ;;
			deleted)
				rc=0; reason=-; why=
				jq -nc --arg n "$(ca_run_name ca)" '{name:$n,status:"deleted"}' >"$tmp/r.json" ;;
			err-then-gone)
				echo gone >"$tmp/ca.current"
				rc=1; reason=API_ERROR; why="container-apps timeout"
				: >"$tmp/r.json" ;;
			*) return 1 ;;
			esac
		else
			echo gone >"$tmp/ca.current"
		fi ;;
	"ar_delete --arg id=our-ar")
		echo gone >"$tmp/ar.current"
		echo '{"id":"our-ar","status":"deleting","operation_id":"delete-ar-op"}' >"$tmp/r.json" ;;
	*) return 1 ;;
	esac
	echo "$1" >>"$tmp/deletes"
}
denied() { if "$@" >/dev/null; then echo "unexpected success: $*" >&2; exit 1; fi; }
echo our-ca >"$tmp/ca.current"
echo our-ar >"$tmp/ar.current"
ca_save ca intent "" ""
denied ca_cleanup_owned
ca_save ca accepted create-op ""
denied ca_cleanup_owned
test ! -e "$tmp/deletes"
# No UID has been bound. A namesake in the intended project has a valid
# UID but lacks our marker from before POST, so it cannot enter the receipt.
echo another-run >"$tmp/ca-description"
denied ca_current_card ca "$(ca_run_name ca)" ""
test "$(jq -r .state "$(ca_receipt_path ca)")" = accepted
ca_marker >"$tmp/ca-description"
ca_current_card ca "$(ca_run_name ca)" ""
ca_save ca owned create-op our-ca >/dev/null
ca_save ar owned registry-op our-ar >/dev/null
echo other-ca >"$tmp/ca.current"
denied ca_cleanup_owned
test ! -e "$tmp/deletes"
echo our-ca >"$tmp/ca.current"
touch "$tmp/gateway"
denied ca_cleanup_owned
test "$(cat "$tmp/deletes")" = ca_delete
test "$(jq -r .state "$(ca_receipt_path ca)")" = deleting
# A replacement after the original DELETE also stays untouched on recovery.
echo foreign-ca >"$tmp/ca.current"
denied ca_cleanup_owned
test "$(cat "$tmp/deletes")" = ca_delete
echo gone >"$tmp/ca.current"; rm "$tmp/gateway"
# AR-2 returns 403 even after deletion. Absence in the list alone cannot
# establish that our deletion succeeded; verify the original operation.
touch "$tmp/ar-forbidden"
for answer in '{"id":"delete-ar-op","done":false}' '{"id":"another-op","done":true}' \
	'{"id":"delete-ar-op","done":true,"resourceId":"other-ar"}' '{"id":"delete-ar-op","done":true,"error":{"code":7}}'; do
	echo "$answer" >"$tmp/ar-op-answer"
	denied ca_cleanup_owned
	test "$(jq -r '[.state,.delete_op]|join(" ")' "$(ca_receipt_path ar)")" = 'deleting delete-ar-op'
done
denied ca_disappeared ar "$(ca_run_name ar)" our-ar ""
echo '{"id":"delete-ar-op","done":true,"resourceId":"our-ar"}' >"$tmp/ar-op-answer"
touch "$tmp/ar-list-failed"
denied ca_cleanup_owned
rm "$tmp/ar-list-failed"
ca_cleanup_owned
ca_cleanup_owned
test "$(cat "$tmp/deletes")" = "$(printf 'ca_delete\nar_delete')"
for kind in ca ar; do test "$(jq -r .state "$(ca_receipt_path "$kind")")" = gone; done

# --- Separate call outcome vs wait-end diagnostics & strict DELETE invariant ---
new_ca_case() {
	rm -f "$(ca_receipt_path ca)" "$(ca_receipt_path ar)" "$tmp/events" "$tmp/deletes" \
		"$tmp/ca-mode" "$tmp/ca-list-still" "$tmp/iam-fail" "$tmp/ca-tc-mode"
	echo our-ca >"$tmp/ca.current"
	echo deleting >"$tmp/ca-tc-mode"
	ca_save ca intent "" ""
	ca_marker >"$tmp/ca-description"
	ca_save ca owned create-op our-ca >/dev/null
}

# 1) Accepted DELETE (status=deleting) is NOT sufficient when GET stays 200 DELETING.
new_ca_case
echo deleting >"$tmp/ca-tc-mode"
echo 200-deleting >"$tmp/ca-mode"
denied ca_cleanup_owned
test "$(jq -r .state "$(ca_receipt_path ca)")" = deleting
grep -q 'ROW	ca_delete вызов	принят	rc=0, status=deleting, op=нет' "$tmp/events"
grep -q 'BAD	ca_delete	вызов: rc=0, status=deleting, op=нет; ожидание: GET HTTP 200 (status=DELETING, исходный ID); остаток возможен' "$tmp/events"

# 2) Even status=deleted from ca_delete is NOT sufficient if GET stays 200.
new_ca_case
echo deleted >"$tmp/ca-tc-mode"
echo 200-deleting >"$tmp/ca-mode"
denied ca_cleanup_owned
test "$(jq -r .state "$(ca_receipt_path ca)")" = deleting
grep -q 'BAD	ca_delete	вызов: rc=0, status=deleted, op=нет; ожидание: GET HTTP 200 (status=DELETING, исходный ID)' "$tmp/events"

# 3) Gateway 404 Route not found is rejected and separately reported as wait reason.
new_ca_case
echo 404-gateway >"$tmp/ca-mode"
denied ca_cleanup_owned
grep -q 'ожидание: GET HTTP 404 не подтверждает удаление приложения (msg=Route not found)' "$tmp/events"

# 4) 403 Forbidden and 499 naming another app are rejected with redacted reason.
new_ca_case
echo 403-forbidden >"$tmp/ca-mode"
denied ca_cleanup_owned
grep -q 'ожидание: GET HTTP 403 (code=7, msg=Forbidden)' "$tmp/events"
new_ca_case
echo 499-other-app >"$tmp/ca-mode"
denied ca_cleanup_owned
grep -q 'ожидание: GET HTTP 499 не подтверждает удаление приложения' "$tmp/events"

# 5) iam_fresh failure is reported distinctly.
new_ca_case
echo 200-deleting >"$tmp/ca-mode"
touch "$tmp/iam-fail"
denied ca_cleanup_owned
grep -q 'ожидание: iam_fresh: не удалось получить IAM-токен' "$tmp/events"

# 6) Valid 499 naming our app STILL fails if ca_list still contains the app.
new_ca_case
echo 499-named >"$tmp/ca-mode"
touch "$tmp/ca-list-still"
denied ca_cleanup_owned
grep -q 'ожидание: GET HTTP 499 подтверждён, но ресурс ещё присутствует в ca_list' "$tmp/events"

# 7) Valid 499 naming our app + absent from ca_list succeeds on sweep without re-sending DELETE.
rm -f "$tmp/ca-list-still"
echo gone >"$tmp/ca.current"
: >"$tmp/events"
ca_cleanup_owned
test "$(wc -l <"$tmp/deletes" | tr -d ' ')" = 1
test "$(jq -r .state "$(ca_receipt_path ca)")" = gone
grep -q 'OK	ca_delete	вызов: повторное ожидание (state=deleting, DELETE не повторялся); ожидание: отсутствие исходного ID подтверждено' "$tmp/events"

# 8) Valid 404 with string code "5" and named container + absent list succeeds even if ca_delete returned rc=1 after dispatch.
new_ca_case
echo err-then-gone >"$tmp/ca-tc-mode"
echo 404-named-str5 >"$tmp/ca-mode"
ca_cleanup_owned
test "$(jq -r .state "$(ca_receipt_path ca)")" = gone
grep -q 'ROW	ca_delete вызов	ошибка	rc=1, reason=API_ERROR (container-apps timeout)' "$tmp/events"
grep -q 'OK	ca_delete	вызов: rc=1, reason=API_ERROR (container-apps timeout); ожидание: отсутствие исходного ID подтверждено' "$tmp/events"

echo 'CA/AR cleanup by receipt: OK'
