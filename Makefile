FUZZTIME ?= 20s

.PHONY: fmt-check vet comments test test-race fuzz corpus check

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

corpus:
	./scripts/fetch-corpus.sh

check: fmt-check vet comments test-race
