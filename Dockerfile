# MCP-сервер evo для Marketplace Cloud.ru (Streamable HTTP).
#
#   docker build -t evo:dev .
#   docker run -e MCP_BEARER_TOKEN=... -e CLOUDRU_KEY_ID=... -e CLOUDRU_SECRET=... \
#     -e CLOUDRU_PROJECT_ID=... -p 8080:8080 evo:dev
#
# Без MCP_BEARER_TOKEN сервер не стартует. kubectl и helm в образе нет:
# k8s_kubectl/k8s_apply/k8s_helm отвечают ошибкой «недоступно в этом режиме».

# Та же версия Go, что в ci.yml и release.yml; поднимать вместе.
ARG GO_VERSION=1.26.10

# Базовые образы закреплены по digest (индекс всех платформ): тег можно
# переписать в реестре, digest — нет. Поднимая GO_VERSION, обновите и digest
# (это делает Dependabot):
#   docker buildx imagetools inspect golang:<версия> --format '{{.Manifest.Digest}}'
FROM golang:${GO_VERSION}@sha256:5c3f3f28a273c9b61698b2d34f18375929790ab1f1d3835d032d21e2b31eae0c AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
# Статический бинарь, как в .goreleaser.yaml: distroless/static без libc.
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/evo ./cmd/evo

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/evo /usr/local/bin/evo
LABEL org.opencontainers.image.title="Cloud.ru Evolution CLI CREW edition" \
      org.opencontainers.image.authors="Timur Shakirbekov, Cloud.ru" \
      org.opencontainers.image.vendor="Cloud.ru" \
      org.opencontainers.image.source="https://github.com/shatim-ops/evo-cli" \
      org.opencontainers.image.licenses="Apache-2.0"
# HOME=/home/nonroot: туда ложится кэш IAM-токена (os.UserCacheDir():
# ~/.cache/evo); рабочий каталог — для локального состояния
# deploy/provision (./.evo/).
WORKDIR /home/nonroot
USER nonroot:nonroot
EXPOSE 8080
# В distroless нет ни shell, ни curl: бинарь сам опрашивает /healthz.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/usr/local/bin/evo", "mcp", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/evo"]
CMD ["mcp", "--http", ":8080"]
