BINARY := subtitle-fetcher
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

ROOT ?= Z:\Shared\Downloads
PORT ?= 8080

ifeq ($(GOOS),windows)
  OUT  := $(BINARY).exe
  RUN  := $(BINARY).exe
  RM   := del /f /q
  NULL := nul
else
  OUT  := $(BINARY)
  RUN  := ./$(BINARY)
  RM   := rm -f
  NULL := /dev/null
endif

VERSION ?= $(shell git describe --tags --always --dirty 2>$(NULL) || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: all build run serve fmt vet lint test clean help
.DEFAULT_GOAL := help

## build   Compile the binary for the current platform
build:
	go build $(LDFLAGS) -o $(OUT) .

## run     Build and run  (pass flags via ARGS=, e.g. make run ARGS="--scan Z:\path")
run: build
	$(RUN) $(ARGS)

## serve   Build and start the web UI  (override with ROOT= and PORT=)
serve: build
	$(RUN) --serve "$(ROOT)" --port $(PORT)

## fmt     Format all Go source files with gofmt
fmt:
	gofmt -w .

## vet     Run go vet
vet:
	go vet ./...

## lint    Run staticcheck (install: go install honnef.co/go/tools/cmd/staticcheck@latest)
lint:
	staticcheck ./...

## test    Run the test suite
test:
	go test -race -count=1 ./...

## clean   Remove compiled binary
clean:
	go clean
	-$(RM) $(OUT)

## help    Show this help
help:
	@echo Usage: make [target]
	@echo   build    Compile the binary
	@echo   run      Build and run        ARGS="--scan Z:\path"
	@echo   serve    Start the web UI     ROOT=path PORT=8080
	@echo   fmt      Format source with gofmt
	@echo   vet      Run go vet
	@echo   lint     Run staticcheck
	@echo   test     Run tests
	@echo   clean    Remove build artifacts

all: build
