BINARY  := subtitle-fetcher
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)

ifeq ($(GOOS),windows)
	BINARY := $(BINARY).exe
endif

.PHONY: all build clean fmt vet test

all: build

build:
	go build -o $(BINARY) .

clean:
	go clean
	rm -f subtitle-fetcher subtitle-fetcher.exe

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./...
