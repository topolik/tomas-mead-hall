FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test ./...
RUN CGO_ENABLED=0 go build -o /mnd ./cmd/mnd

# mnd is a static binary with no runtime deps; session dirs are mounted
# read-only, data/ and brain/ read-write (uid mapped via compose `user:`).
FROM scratch
COPY --from=builder /mnd /mnd
WORKDIR /app
ENTRYPOINT ["/mnd"]
