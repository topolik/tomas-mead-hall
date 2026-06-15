FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /dsh ./cmd/dsh

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata sqlite
WORKDIR /app
COPY --from=builder /dsh /app/dsh
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/dsh"]
