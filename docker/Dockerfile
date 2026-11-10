# Фронтенд Mini App: собирается в webapp/dist и встраивается в бинарь (go:embed).
FROM node:22-alpine AS web
WORKDIR /web
COPY webapp/package.json webapp/package-lock.json ./
RUN npm ci
COPY webapp/ ./
# Ссылки «Добавить бота в группу» и «Ссылка на игру» в Mini App. Пустые — кнопки скрыты.
# В Coolify задаются как build-переменные (Build Variable).
ARG VITE_BOT_USERNAME=""
ARG VITE_APP_SHORT_NAME=""
ENV VITE_BOT_USERNAME=$VITE_BOT_USERNAME \
    VITE_APP_SHORT_NAME=$VITE_APP_SHORT_NAME
RUN npm run build

FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./webapp/dist
RUN CGO_ENABLED=0 go build -o /bot ./cmd/poker-bank

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bot /bot
# 8080 — API, Mini App и webhook; 9090 — служебный (/metrics, /ready), наружу не публикуется.
EXPOSE 8080 9090
ENTRYPOINT ["/bot"]
