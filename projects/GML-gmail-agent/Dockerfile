FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test ./...
RUN CGO_ENABLED=0 go build -o /gml ./cmd/gml

# gws install stage
FROM node:22-bookworm-slim AS gws-installer
RUN npm install -g @googleworkspace/cli

# Final image — gws ELF binary requires GLIBC 2.39; ubuntu:24.04 ships 2.39
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=gws-installer /usr/local/lib/node_modules/@googleworkspace/cli/bin/gws /usr/local/bin/gws
COPY --from=builder /gml /usr/local/bin/gml

RUN useradd -r -m -s /usr/sbin/nologin gml
USER gml
WORKDIR /app
ENTRYPOINT ["gml"]
