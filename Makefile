FUZZTIME ?= 20s
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)

.PHONY: fmt-check vet comments test test-race fuzz corpus build image check

fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

vet:
	go vet ./...

comments:
	go run scripts/check-comments.go

test:
	go test ./...

test-race:
	go test -race ./...

fuzz:
	go test -run '^$$' -fuzz FuzzLoad -fuzztime $(FUZZTIME) ./core/source/
	go test -run '^$$' -fuzz FuzzParseTemplate -fuzztime $(FUZZTIME) ./core/expr/
	go test -run '^$$' -fuzz FuzzExpand -fuzztime $(FUZZTIME) ./core/matrix/
	go test -run '^$$' -fuzz FuzzCompare -fuzztime $(FUZZTIME) ./core/semdiff/
	go test -run '^$$' -fuzz FuzzCommand -fuzztime $(FUZZTIME) ./internal/action/

corpus:
	./scripts/fetch-corpus.sh

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/quanto ./cmd/quanto

image:
	podman build -f deploy/Containerfile --ignorefile deploy/.containerignore --build-arg VERSION=$(VERSION) -t localhost/quanto:$(VERSION) -t localhost/quanto:latest .

check: fmt-check vet comments test-race
