.PHONY: build test docker-build run watch clean

build:
	go build ./cmd/dsh

test:
	go test ./...

docker-build:
	docker build -t dsh .

run:
	docker compose up --build

# Auto-rebuild + recreate the container on any source change (Compose Watch).
watch:
	docker compose up --build -d && docker compose watch

clean:
	go clean
	rm -f dsh
