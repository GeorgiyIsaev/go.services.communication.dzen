# Этап сборки
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Копируем go.mod и go.sum и скачиваем зависимости
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь исходный код
COPY . .

# Собираем бинарник (укажите правильный путь к вашему main-пакету, например, ./cmd/ClickStorage)
RUN go build -o /app/myapp ./cmd/ClickStorage

# Финальный этап (минимальный образ)
FROM alpine:latest

WORKDIR /app

# Копируем собранный бинарник из предыдущего этапа
COPY --from=builder /app/myapp /app/myapp

# Копируем папку с миграциями (если она нужна на этапе выполнения)
COPY migrations /app/migrations

# Открываем порт, который использует приложение (замените на свой, например, 8080)
EXPOSE 8080

# Команда запуска
CMD ["/app/myapp"]