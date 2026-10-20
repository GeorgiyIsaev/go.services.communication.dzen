# ---------- Этап сборки ----------
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/click-imitator ./app/ClickImitator/cmd

# ---------- Runtime ----------
FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/click-imitator /app/click-imitator

# Сценарии вшиваем в образ — они нужны для scenario-режима.
COPY --from=builder /src/app/ClickImitator/cmd/scenarios /app/scenarios

# SCENARIO_DIR по умолчанию в коде = "cmd/scenarios" (относительный путь).
# В образе бинарник лежит в /app, а сценарии — в /app/scenarios,
# поэтому явно задаём правильный абсолютный путь.
ENV SCENARIO_DIR=/app/scenarios

ENTRYPOINT ["/app/click-imitator"]