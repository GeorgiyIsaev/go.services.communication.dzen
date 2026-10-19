# syntax=docker/dockerfile:1

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
    -o /out/click-dashboard ./app/ClickDashboard/cmd

# ---------- Runtime ----------
FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/click-dashboard /app/click-dashboard

EXPOSE 8085

ENTRYPOINT ["/app/click-dashboard"]