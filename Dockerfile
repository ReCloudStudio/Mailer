# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.26-alpine AS build

WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Build the static binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mailer .

# ---- runtime stage ----
FROM alpine:3.20

# CA certificates are required for TLS (IMAPS + Telegram/Discord HTTPS).
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -u 10001 mailer

WORKDIR /app
COPY --from=build /out/mailer /usr/local/bin/mailer

# config.yaml and state.db are mounted/persisted via /app.
USER mailer

ENTRYPOINT ["mailer"]
CMD ["-config", "/app/config.yaml"]
