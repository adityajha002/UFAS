# Stage 1: Build the Admin React WebUI
FROM node:22-alpine AS ui-builder
WORKDIR /src/admin/server/webui
COPY admin/server/webui/package*.json ./
RUN npm ci
COPY admin/server/webui ./
RUN npm run build

# Stage 2: Build the Go Binaries (attendance server and admin server)
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache build-base
WORKDIR /src

# Download dependencies
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download

COPY admin/server/go.mod admin/server/go.sum ./admin/server/
RUN cd admin/server && go mod download

# Build server
COPY server/ ./server/
RUN cd server && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/server .

# Build admin-server with embedded UI
COPY admin/server/ ./admin/server/
COPY --from=ui-builder /src/admin/server/webui/dist ./admin/server/webui/dist
RUN cd admin/server && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/admin-server .

# Stage 3: Final lightweight runtime container
FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget sed
WORKDIR /app

COPY --from=builder /out/server /usr/local/bin/server
COPY --from=builder /out/admin-server /usr/local/bin/admin-server

COPY server/config.docker.yaml /etc/ufas/config.server.yaml
COPY admin/server/config.docker.yaml /etc/ufas/config.admin.yaml
COPY scripts/render-entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/server /usr/local/bin/admin-server

RUN mkdir -p /data && chmod 0777 /data

EXPOSE 8080 10000
CMD ["/usr/local/bin/entrypoint.sh"]
