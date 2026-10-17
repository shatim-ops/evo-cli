#!/usr/bin/env bash
# Smoke-тест MCP-сервера evo по HTTP: job docker в ci.yml и job image в
# release.yml вызывают один и тот же скрипт, чтобы проверки не разошлись
# (релизный аудит, R-01: release.yml слал POST без Content-Type, сервер с #87
# отвечает на такой 415, и образ не публиковался).
#
#   mcp-smoke.sh <адрес> <bearer-токен> [<ожидаемая версия>]
#
# Сервер уже запущен (docker run … -p 8080:8080). Облако не нужно:
# initialize, tools/list и отказ k8s_kubectl без kubectl в облако не ходят.
set -euo pipefail
base=${1:?адрес сервера, например http://localhost:8080}
token=${2:?bearer-токен сервера}
want_version=${3:-}

for _ in $(seq 1 30); do curl -sf "$base/healthz" >/dev/null && break; sleep 1; done
curl -sf "$base/healthz"

code=$(curl -s -o /dev/null -w '%{http_code}' "$base/mcp")
echo "без токена: $code"; [ "$code" = 401 ]

# Тело — JSON, как у любого клиента MCP: без Content-Type: application/json
# сервер отвечает 415 (защита от CSRF). Accept не нужен: на POST без
# text/event-stream в Accept сервер отвечает простым JSON.
rpc() {
  curl -sf -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "$1" "$base/mcp"
}

init=$(rpc '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}')
v=$(jq -r '[.result.protocolVersion, .result.serverInfo.name] | join(" ")' <<<"$init")
echo "protocolVersion, serverInfo.name: $v"; [ "$v" = "2025-06-18 evo" ]
if [ -n "$want_version" ]; then
  got=$(jq -r '.result.serverInfo.version' <<<"$init")
  echo "serverInfo.version: $got"; [ "$got" = "$want_version" ]
fi

n=$(rpc '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | jq '.result.tools | length')
echo "tools/list: $n"; [ "$n" -gt 0 ]

# kubectl в образе нет: инструмент отвечает понятной ошибкой.
rpc '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"k8s_kubectl","arguments":{"cluster_id":"x","args":"get pods"}}}' |
  jq -e '.result.isError and (.result.content[0].text | contains("not available in this mode"))'
