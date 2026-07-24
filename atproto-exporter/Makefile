BINARY := atproto-exporter
PKG := ./...

# The Jetstream /subscribe compression dictionary, embedded via //go:embed.
ZSTD_DICT_PATH := internal/jetstream/zstd_dict.bin
ZSTD_DICT_URL := https://raw.githubusercontent.com/bluesky-social/jetstream/main/internal/subscribe/zstd_dictionary

.PHONY: build test race vet fmt lint run docker compose update-zstd-dict tidy

build:
	go build -o $(BINARY) ./cmd/atproto-exporter

test:
	go test $(PKG)

race:
	go test -race -cover $(PKG)

vet:
	go vet $(PKG)

fmt:
	gofmt -w .

lint: vet
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

run:
	go run ./cmd/atproto-exporter

docker:
	docker build -t $(BINARY) .

compose:
	docker compose -f deploy/docker-compose.yml up --build

tidy:
	go mod tidy

# Refresh the embedded Jetstream zstd dictionary (run if Bluesky rotates it).
update-zstd-dict:
	curl -fsSL "$(ZSTD_DICT_URL)" -o "$(ZSTD_DICT_PATH)"
	@echo "updated $(ZSTD_DICT_PATH) ($$(wc -c < $(ZSTD_DICT_PATH)) bytes)"
