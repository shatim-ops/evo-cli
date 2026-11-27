#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Container Apps deletes by name. The owner isolates this project for the run.
# Keep the original operation and UID across both cleanup steps on the runner.

ca_receipt_path() { printf '%s/evo-e2e-ca-%s-%s.json' "${RUNNER_TEMP:-$tmp}" "$RUN" "$1"; }
ca_run_name() { case "$1" in ar) echo "evo-e2e-ca-$RUN" ;; ca) echo "evo-e2e-ca-$RUN-app" ;; *) return 1 ;; esac; }
ca_marker() {
	jq -er --arg r "$RUN" --arg p "$PROJECT" 'select(.run==$r and .project==$p) |
		.marker | strings | select(startswith("evo-e2e-"+$r+"-") and (split("-")[-1]|test("^[a-f0-9]{32}$")))' "$(ca_receipt_path ca)"
}
ca_save() {
	local kind=$1 state=$2 op=$3 id=$4 delete_op=${5:-} file name marker=
	file=$(ca_receipt_path "$kind")
	name=$(ca_run_name "$kind") || return 1
	[[ -n ${PROJECT:-} && $RUN =~ ^r[0-9]+a[0-9]+$ ]] || return 1
	[ "$state" != intent ] || [ ! -e "$file" ] || return 1
	if [[ $kind = ca ]]; then
		if [[ $state = intent ]]; then
			marker=$(openssl rand -hex 16) && [[ $marker =~ ^[a-f0-9]{32}$ ]] || return 1
			marker="evo-e2e-$RUN-$marker"
		else marker=$(ca_marker) || return 1; fi
	fi
	jq -nc --arg run "$RUN" --arg project "$PROJECT" --arg name "$name" --arg state "$state" \
		--arg op "$op" --arg id "$id" --arg d "$delete_op" --arg m "$marker" '{run:$run,project:$project,name:$name,state:$state,op:$op,id:$id}
		+ (if $d!="" then {delete_op:$d} else {} end) + (if $m!="" then {marker:$m} else {} end)' \
		>"$file.new" && mv "$file.new" "$file" || return 1
	if [[ $state == owned ]]; then
		mask "$id"
		printf 'Квитанция %s, SHA-256 ID: %s\n' "$kind" "$(sha "$id")"
	fi
}

ca_redact() {
	if declare -F redact >/dev/null 2>&1; then
		redact
	else
		cat
	fi
}

ca_row() {
	if declare -F row >/dev/null 2>&1; then
		row "$@"
	fi
}

ca_api_err_note() {
	local name=$1 id=$2 err_code err_msg raw
	if [ ! -s "$tmp/api.json" ]; then
		printf 'пустой ответ'
		return 0
	fi
	err_code=$(jq -r 'if type=="object" and .code != null then (.code | tostring) else empty end' "$tmp/api.json" 2>/dev/null || true)
	err_msg=$(jq -r 'if type=="object" then (.message // .error // empty | if type=="string" then . else tojson end) else empty end' "$tmp/api.json" 2>/dev/null || true)
	if [ -z "$err_msg" ]; then
		raw=$(head -c 160 "$tmp/api.json" | tr '\r\n' '  ')
		err_msg=$raw
	fi
	if [ -n "$name" ]; then err_msg=${err_msg//"$name"/<name>}; fi
	if [ -n "$id" ]; then err_msg=${err_msg//"$id"/<id>}; fi
	err_msg=$(printf '%s' "$err_msg" | tr '\r\n' '  ' | ca_redact | cut -c1-120)
	if [ -n "$err_code" ]; then
		printf 'code=%s, msg=%s' "$(printf '%s' "$err_code" | ca_redact | cut -c1-40)" "${err_msg:-пусто}"
	else
		printf 'msg=%s' "${err_msg:-пусто}"
	fi
}

# Every page through the product's list client. Fail closed on an incomplete
# record, a conflicting name or a duplicate UID.
ca_list_identity() {
	local kind=$1 name=$2 id=$3 expected=$4 field rows rc_list
	ca_list_reason=
	case "$kind" in ca) field=containers ;; ar) field=registries ;; *) ca_list_reason="неизвестный вид ресурса"; return 1 ;; esac
	if ! rows=$(evo --project-id "$PROJECT" tools call "${kind}_list" --json 2>"$tmp/lst.err"); then
		rc_list=$?
		ca_list_reason="${kind}_list завершился с кодом $rc_list"
		return 1
	fi
	if jq -e --arg f "$field" --arg n "$name" --arg id "$id" --arg expected "$expected" '
		.[$f] | type == "array" and all(.[]; (.id|type=="string" and length>0) and (.name|type=="string" and length>0)) and
		([.[]|select(.name==$n or .id==$id)] | if $expected=="absent" then length==0
			else length==1 and .[0].id==$id and .[0].name==$n end)' <<<"$rows" >/dev/null 2>&1; then
		return 0
	fi
	if ! jq -e --arg f "$field" '.[$f] | type == "array" and all(.[]; (.id|type=="string" and length>0) and (.name|type=="string" and length>0))' <<<"$rows" >/dev/null 2>&1; then
		ca_list_reason="${kind}_list вернул неполный или некорректный список"
	elif [ "$expected" = absent ]; then
		ca_list_reason="ресурс ещё присутствует в ${kind}_list (по имени или исходному ID)"
	else
		ca_list_reason="запись в ${kind}_list не совпадает с квитанцией по имени и исходному ID"
	fi
	return 1
}

ca_current_card() {
	local kind=$1 name=$2 id=$3 arg marker=
	case "$kind" in ca) arg="name=$name"; marker=$(ca_marker) || return 1 ;; ar) arg="id=$id" ;; *) return 1 ;; esac
	evo --project-id "$PROJECT" tools call "${kind}_get" --arg "$arg" --json >"$tmp/ca-card" 2>"$tmp/lst.err" &&
		jq -e --arg k "$kind" --arg p "$PROJECT" --arg n "$name" --arg id "$id" --arg m "$marker" '
			($k=="ar" or .description==$m) and (.projectId==$p or ($k=="ar" and .projectId==null)) and .name==$n and (.id|type=="string" and test("^[A-Za-z0-9-]{1,80}$")) and
			($id=="" or .id==$id)' "$tmp/ca-card" >/dev/null
}

ca_disappeared() {
	local kind=$1 name=$2 id=$3 delete_op=${4:-} url st id_match op_json op_diag
	ca_wait_reason=
	if ! iam_fresh; then
		ca_wait_reason="iam_fresh: не удалось получить IAM-токен"
		return 1
	fi
	case "$kind" in
	ca) url="https://containers.api.cloud.ru/v2/containers/$name?projectId=$PROJECT" ;;
	ar) url="https://ar.api.cloud.ru/v1/registries/$id?projectId=$PROJECT" ;;
	*) ca_wait_reason="неизвестный вид ресурса"; return 1 ;;
	esac
	api GET "$url"
	if [ "${code:-}" = 000 ]; then
		ca_wait_reason="GET HTTP 000 (curl rc=${API_RC:-?})"
		return 1
	fi
	case "$kind:$code" in
	ca:499|ca:404)
		if ! jq -e --arg n "$name" --arg http "$code" --arg sq "'" '
			((.code == null) or .code == 5 or .code == "5" or
				((.code | tostring | ascii_upcase) == "NOT_FOUND") or
				((.code | tostring) == $http)) and
			(.message | type == "string") and
			((.message | gsub("^\\s+|\\s+$"; "") | ascii_downcase | rtrimstr(".")) as $m |
				($n | ascii_downcase) as $ln |
				$m == "serverless with name is not found" or
				$m == ("serverless with name " + $ln + " is not found") or
				$m == ("serverless with name \"" + $ln + "\" is not found") or
				$m == ("serverless with name " + $sq + $ln + $sq + " is not found") or
				$m == "container not found" or
				$m == ("container " + $ln + " not found") or
				$m == ("container \"" + $ln + "\" not found") or
				$m == ("container " + $sq + $ln + $sq + " not found") or
				$m == ("container " + $ln + " is not found") or
				$m == ("container \"" + $ln + "\" is not found") or
				$m == ("container " + $sq + $ln + $sq + " is not found"))
		' "$tmp/api.json" >/dev/null 2>&1; then
			ca_wait_reason="GET HTTP $code не подтверждает удаление приложения ($(ca_api_err_note "$name" "$id"))"
			return 1
		fi ;;
	ar:404)
		if ! jq -e --arg id "$id" '(.code==5 or .code=="5" or .code=="NOT_FOUND") and
			((.message|ascii_downcase|rtrimstr(".")) as $m | $m=="registry not found" or $m=="registry does not exist" or
			$m==("registry "+$id+" not found"))' "$tmp/api.json" >/dev/null 2>&1; then
			ca_wait_reason="GET HTTP 404 не подтверждает удаление реестра ($(ca_api_err_note "$name" "$id"))"
			return 1
		fi ;;
	ar:403)
		# AR-2: the registry card can be forbidden after a successful delete.
		# Corroborate the complete project list with the exact delete operation.
		if [[ ! $delete_op =~ ^[A-Za-z0-9-]{1,80}$ ]]; then
			ca_wait_reason="GET HTTP 403, нет валидного delete_op для сверки ar_operation"
			return 1
		fi
		if ! op_json=$(evo --project-id "$PROJECT" tools call ar_operation --arg operation_id="$delete_op" --json 2>"$tmp/lst.err"); then
			ca_wait_reason="GET HTTP 403, ошибка чтения ar_operation"
			return 1
		fi
		if ! jq -e --arg op "$delete_op" --arg id "$id" '.id==$op and .done==true and .error==null and
			(.resourceId==null or .resourceId==$id)' <<<"$op_json" >/dev/null 2>&1; then
			op_diag=$(jq -r --arg op "$delete_op" --arg id "$id" '
				"done=" + ((.done // "?") | tostring) +
				", id_ok=" + ((.id == $op) | tostring) +
				", res_ok=" + ((.resourceId == null or .resourceId == $id) | tostring) +
				", err=" + (if .error == null then "нет" else "есть" end)
			' <<<"$op_json" 2>/dev/null || echo "не JSON")
			ca_wait_reason="GET HTTP 403, ar_operation не подтвердила удаление ($op_diag)"
			return 1
		fi ;;
	*:200)
		st=$(jq -r '.status // .state // "?"' "$tmp/api.json" 2>/dev/null || echo "?")
		id_match=$(jq -r --arg id "$id" 'if type!="object" then "не JSON" elif (.id // "") == "" then "нет ID" elif .id == $id then "исходный ID" else "другой ID" end' "$tmp/api.json" 2>/dev/null || echo "не JSON")
		ca_wait_reason="GET HTTP 200 (status=$(printf '%s' "$st" | ca_redact | cut -c1-40), $id_match)"
		return 1 ;;
	*)
		ca_wait_reason="GET HTTP ${code:-?} ($(ca_api_err_note "$name" "$id"))"
		return 1 ;;
	esac
	# Absence of the name and of the original UID are both required.
	if ! ca_list_identity "$kind" "$name" "$id" absent; then
		ca_wait_reason="GET HTTP $code подтверждён, но ${ca_list_reason:-ресурс ещё присутствует в ${kind}_list}"
		return 1
	fi
	ca_wait_reason=
	return 0
}

ca_cleanup_one() {
	local kind=$1 file name state op id delete_op call_status call_op
	ca_call_summary=
	ca_wait_reason=
	ca_cleanup_detail=
	file=$(ca_receipt_path "$kind")
	[ -e "$file" ] || return 0
	name=$(ca_run_name "$kind") || { ca_cleanup_detail="неизвестный вид ресурса"; return 1; }
	if ! jq -e --arg r "$RUN" --arg p "$PROJECT" --arg n "$name" '
		.run==$r and .project==$p and .name==$n and all(.op,.id;type=="string" and test("^[A-Za-z0-9-]{1,80}$"))' "$file" >/dev/null; then
		ca_cleanup_detail="квитанция $kind не прошла сверку (run/project/name/op/id)"
		return 1
	fi
	state=$(jq -r .state "$file"); op=$(jq -r .op "$file"); id=$(jq -r .id "$file")
	delete_op=$(jq -r '.delete_op // empty' "$file")
	case "$state" in
	gone) return 0 ;;
	owned)
		if ! ca_list_identity "$kind" "$name" "$id" present; then
			ca_cleanup_detail="до удаления: ${ca_list_reason:-список не подтвердил исходный ID}"
			return 1
		fi
		if ! ca_current_card "$kind" "$name" "$id"; then
			ca_cleanup_detail="до удаления: карточка ${kind}_get не подтвердила исходный ID/маркер"
			return 1
		fi
		ca_save "$kind" deleting "$op" "$id" || { ca_cleanup_detail="не удалось записать квитанцию deleting"; return 1; }
		if ! ca_current_card "$kind" "$name" "$id"; then
			ca_cleanup_detail="перед вызовом удаления: карточка ${kind}_get не подтвердила исходный ID/маркер"
			return 1
		fi
		: >"$tmp/r.json"
		rc=0
		reason=
		why=
		call_status=
		call_op=
		if [ "$kind" = ca ]; then
			tc ca_delete --arg name="$name" --arg wait=true
			if [[ $rc = 0 ]]; then
				call_status=$(jq -r '.status // empty' "$tmp/r.json" 2>/dev/null || true)
				call_op=$(jq -er '.operation_id | strings | select(test("^[A-Za-z0-9-]{1,80}$"))' "$tmp/r.json" 2>/dev/null) || call_op=
				if [ -n "$call_op" ]; then
					delete_op=$call_op
					ca_save ca deleting "$op" "$id" "$delete_op" || { ca_cleanup_detail="не удалось обновить квитанцию deleting"; return 1; }
				fi
			fi
		else
			tc ar_delete --arg id="$id"
			if [[ $rc = 0 ]]; then
				call_status=$(jq -r '.status // empty' "$tmp/r.json" 2>/dev/null || true)
				delete_op=$(jq -er --arg id "$id" 'select(.id==$id and (.status=="deleted" or .status=="deleting")) |
					.operation_id | strings | select(test("^[A-Za-z0-9-]{1,80}$"))' "$tmp/r.json" 2>/dev/null) || delete_op=
				call_op=$delete_op
				ca_save ar deleting "$op" "$id" "$delete_op" || { ca_cleanup_detail="не удалось обновить квитанцию deleting"; return 1; }
			fi
		fi
		if [[ $rc = 0 ]]; then
			ca_call_summary="rc=0, status=${call_status:-?}, op=$([ -n "$call_op" ] && echo есть || echo нет)"
			ca_row "${kind}_delete вызов" "принят" "$ca_call_summary"
		else
			ca_call_summary="rc=${rc:-?}, reason=${reason:-?}${why:+ ($why)}"
			ca_row "${kind}_delete вызов" "ошибка" "$ca_call_summary"
		fi
		# Accepted DELETE (or an error after dispatch) is never enough on its own:
		# final absence must still be confirmed via direct GET and full list.
		;;
	deleting)
		ca_call_summary="повторное ожидание (state=deleting, DELETE не повторялся)"
		;;
	*)
		ca_cleanup_detail="неизвестное состояние квитанции $kind: $state"
		return 1 ;;
	esac
	if ! poll 600 20 ca_disappeared "$kind" "$name" "$id" "$delete_op"; then
		ca_cleanup_detail="вызов: $ca_call_summary; ожидание: ${ca_wait_reason:-таймаут без подробностей}"
		return 1
	fi
	ca_save "$kind" gone "$op" "$id" "$delete_op" || { ca_cleanup_detail="не удалось записать квитанцию gone"; return 1; }
	ok "${kind}_delete" "вызов: $ca_call_summary; ожидание: отсутствие исходного ID подтверждено (GET + ${kind}_list), SHA-256 $(sha "$id")"
	cov "${kind}_delete"
}

ca_cleanup_owned() {
	local kind msg
	for kind in ca ar; do
		if ! ca_cleanup_one "$kind"; then
			msg="${ca_cleanup_detail:+$ca_cleanup_detail; }остаток возможен, проверьте квитанцию этого запуска"
			bad "${kind}_delete" "$msg"
			return 1
		fi
	done
}
