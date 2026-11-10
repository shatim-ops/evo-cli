#!/usr/bin/env bash
# Ручная проба входа evo через браузер (B-5, docs/internal/briefs/login-probe.md).
# Ничего не создаёт. Проверка — TestLoginWorld_ProbeScript на мире B-2.
# Печатает только то, что можно записать в отчёт: коды ответов, да/нет,
# имена полей, счётчики. Токены, код и verifier остаются во временном
# каталоге и удаляются при выходе.
set -euo pipefail
ISSUER="${ISSUER:-https://id.cloud.ru/auth/system}"
ORG="${ORG:-https://organization.api.cloud.ru}"
PORT=18386
REDIRECT="http://127.0.0.1:$PORT/callback"
for c in curl jq openssl python3; do command -v "$c" >/dev/null || { echo "нет $c"; exit 1; }; done
# EXTRA — параметры повторного входа к запросу авторизации, для пробы
# prompt=login и max_age (решение владельца 10.11, план 1.8): только
# «prompt=login», «max_age=N» или оба через «&»; иначе отказ до запросов.
EXTRA="${EXTRA:-}"
if [ -n "$EXTRA" ] && ! printf '%s' "$EXTRA" | grep -Eqx '(prompt=login|max_age=[0-9]{1,6})(&(prompt=login|max_age=[0-9]{1,6}))?'; then
	echo "EXTRA: только prompt=login, max_age=N или оба через &"
	exit 1
fi
umask 077
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
verifier=$(openssl rand -base64 48 | tr -d '=+/\n' | cut -c1-64)
challenge=$(printf %s "$verifier" | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')
state=$(openssl rand -hex 16)
nonce=$(openssl rand -hex 16)

echo "== 1. Настройки провайдера (discovery)"
http=$(curl -sS -o "$work/d.json" -w '%{http_code}' "$ISSUER/.well-known/openid-configuration")
jq -c --arg i "$ISSUER" --argjson h "$http" '{http: $h, issuer_ok: (.issuer == $i),
  s256: ((.code_challenge_methods_supported // []) | index("S256") != null),
  has_endpoints: ([.authorization_endpoint, .token_endpoint, .jwks_uri] | all(. != null)),
  prompt_values: (.prompt_values_supported // null), auth_time_claim: ((.claims_supported // []) | index("auth_time") != null)}' "$work/d.json"
auth=$(jq -r .authorization_endpoint "$work/d.json")
token=$(jq -r .token_endpoint "$work/d.json")

echo "== 2. Вход в браузере"
python3 -c "import socket; s=socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); s.bind(('127.0.0.1', $PORT)); s.close()" 2>/dev/null ||
	{ echo "порт $PORT на 127.0.0.1 занят другой программой — закройте её и запустите снова"; exit 1; }
python3 - "$work/cb.json" "$PORT" <<'PY' &
import http.server, json, sys, urllib.parse as u
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass  # the request line carries the code
    def do_GET(self):
        q = u.parse_qs(u.urlparse(self.path).query)
        json.dump(q, open(sys.argv[1], "w"))
        self.send_response(200); self.end_headers()
        self.wfile.write("Готово, вкладку можно закрыть.".encode())
s = http.server.HTTPServer(("127.0.0.1", int(sys.argv[2])), H)
s.timeout = 300
s.handle_request()
PY
listener=$!
enc=$(jq -rn --arg r "$REDIRECT" '$r|@uri')
echo "Откройте эту ссылку в браузере на этой же машине и войдите (5 минут):"
echo "$auth?response_type=code&client_id=evo_cmcp&scope=openid&redirect_uri=$enc&state=$state&nonce=$nonce&code_challenge=$challenge&code_challenge_method=S256${EXTRA:+&$EXTRA}"
wait "$listener" || true
[ -s "$work/cb.json" ] || { echo "браузер не вернулся на 127.0.0.1:$PORT за 5 минут"; exit 1; }
jq -c --arg s "$state" '{params: keys, state_ok: (.state[0] == $s), error: (.error[0] // null)}' "$work/cb.json"
code=$(jq -r '.code[0] // empty' "$work/cb.json")
[ -n "$code" ] || { echo "кода нет — вход не завершён (см. error выше)"; exit 1; }

echo "== 3. Обмен кода на токен"
exchange() {
	curl -sS -o "$1" -w '%{http_code}' -X POST "$token" -d grant_type=authorization_code -d client_id=evo_cmcp \
		--data-urlencode "code=$code" --data-urlencode "redirect_uri=$REDIRECT" -d "code_verifier=$verifier"
}
http=$(exchange "$work/t.json")
jq -c --argjson h "$http" '{http: $h, fields: keys, expires_in, token_type}' "$work/t.json"
http=$(exchange "$work/t2.json")
jq -c --argjson h "$http" '{repeat_same_code_http: $h, error}' "$work/t2.json" 2>/dev/null || echo "{\"repeat_same_code_http\": $http}"

echo "== 4. ID token (только проверки, без значений)"
jq -r '.id_token // empty' "$work/t.json" | cut -d. -f2 |
	python3 -c 'import sys,base64; p=sys.stdin.read().strip(); sys.stdout.buffer.write(base64.urlsafe_b64decode(p+"="*(-len(p)%4)))' |
	jq -c --arg n "$nonce" --arg i "$ISSUER" --argjson now "$(date +%s)" '{iss_ok: (.iss == $i), aud, nonce_ok: (.nonce == $n), lifetime_s: (.exp - .iat),
		auth_age_s: (if .auth_time then $now - .auth_time else null end), claims: keys}' ||
	echo "ID token не разобран"

echo "== 5. Организации и проекты тем же токеном"
tok=$(jq -r .access_token "$work/t.json")
for p in customers projects; do
	http=$(curl -sS -o "$work/$p.json" -w '%{http_code}' -H "Authorization: Bearer $tok" "$ORG/v3/$p")
	jq -c --arg p "$p" --argjson h "$http" '{($p): $h, count: ([.[]? | arrays | length] | add)}' "$work/$p.json" 2>/dev/null ||
		echo "{\"$p\": $http}"
done
echo "== Готово: временные файлы удалены, в облаке ничего не создано."
