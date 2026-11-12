FROM docker.io/library/node:24-alpine AS frontend
WORKDIR /build/web
RUN npm install --global pnpm@12.16.1
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM docker.io/library/golang:1.25-alpine AS backend
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /build/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ticket-hub ./cmd/server

FROM docker.io/library/alpine:3.23
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 app \
    && adduser -D -u 10001 -G app app \
    && mkdir /data && chown app:app /data
COPY --from=backend /ticket-hub /usr/local/bin/ticket-hub
USER 10001:10001
WORKDIR /data
ENV LISTEN_ADDR=:8080 DB_TYPE=sqlite DB_DSN=/data/ticket-hub.db
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=5 \
  CMD wget -q -T 3 -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/ticket-hub"]
