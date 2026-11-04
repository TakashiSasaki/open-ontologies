.PHONY: build check
REVISION := $(shell git rev-parse --verify HEAD 2>/dev/null || echo unknown)
MODIFIED := $(if $(shell git status --porcelain),true,false)
build:
	CGO_ENABLED=0 go build -trimpath -buildvcs=true -ldflags '-X main.revision=$(REVISION) -X main.modified=$(MODIFIED)' -o bin/open-ontologies-mcp-adapter ./cmd/open-ontologies-mcp-adapter
check:
	test -z "$$(gofmt -l cmd internal)"
	go test -race ./...
	go vet ./...
