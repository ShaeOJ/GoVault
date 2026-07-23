# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gostratum-edge ./cmd/edgenode/

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN addgroup -S gostratum && adduser -S gostratum -G gostratum

WORKDIR /app
COPY --from=build /gostratum-edge .

# data/ is the config + DB volume
RUN mkdir -p /app/data && chown gostratum:gostratum /app/data
VOLUME ["/app/data"]

USER gostratum

# Stratum (miners connect here)
EXPOSE 3333
# HTTP dashboard
EXPOSE 8080

ENTRYPOINT ["/app/gostratum-edge"]
