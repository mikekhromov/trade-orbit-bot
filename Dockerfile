FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/telegram-bot ./cmd/bot

FROM alpine:3.22
RUN addgroup -S trading && adduser -S trading -G trading
USER trading
COPY --from=build /out/telegram-bot /usr/local/bin/telegram-bot
EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD wget -qO- http://127.0.0.1:8081/healthz || exit 1
ENTRYPOINT ["telegram-bot"]
