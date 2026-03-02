.PHONY: build run test lint clean docker-build

BINARY=gh-webhook-handler
VERSION?=dev
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/server

run: build
	./bin/$(BINARY) --config configs/ --addr :8080

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ deliveries.db

docker-build:
	docker build -t $(BINARY):$(VERSION) .
