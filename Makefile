BINARY := subtitle-fetcher
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

ROOT ?= Z:\Shared\Downloads
PORT ?= 9191

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

.PHONY: all build run serve fmt vet lint test test-all retest clean help docker-build docker-run docker-clean
.DEFAULT_GOAL := help

## build   Run the full test suite then compile the binary for the current platform
build: test-all
	go build $(LDFLAGS) -o $(OUT) ./src

## run     Build and run  (pass flags via ARGS=, e.g. make run ARGS="--scan Z:\path")
run: build
	$(RUN) $(ARGS)

## serve   Build and start the web UI  (override with ROOT= and PORT=)
serve: build
	$(RUN) --serve "$(ROOT)" --port $(PORT)

## fmt     Format all Go source files with gofmt
fmt:
	gofmt -w ./src

## vet     Run go vet
vet:
	go vet ./...

## lint    Run staticcheck (install: go install honnef.co/go/tools/cmd/staticcheck@latest)
lint:
	staticcheck ./...

## test     Fast inner-loop tests (cached; skips fsnotify integration tests)
test:
	go test -short ./...

## test-all Full suite incl. fsnotify watcher integration tests (cached)
test-all:
	go test ./...

## retest   Force a full re-run, bypassing the test cache
retest:
	go test -count=1 ./...

## clean   Remove compiled binaries (both platform names)
clean:
	go clean
	-$(RM) $(BINARY) $(BINARY).exe

## docker-build  Build the docker image
docker-build:
	docker build -t $(BINARY):latest .

## docker-run    Build then run the docker image (with default binds)
docker-run: docker-build
	docker run --rm -it -p $(PORT):$(PORT) -e PORT=$(PORT) -v "$(ROOT)":/media $(BINARY):latest --serve /media

## docker-clean  Remove the docker image
docker-clean:
	docker rmi $(BINARY):latest

## help    Show this help
help:
	@echo Usage: make [target]
	@echo   build    Compile the binary
	@echo   run      Build and run        ARGS="--scan Z:\path"
	@echo   serve    Start the web UI     ROOT=path PORT=8080
	@echo   fmt      Format source with gofmt
	@echo   vet      Run go vet
	@echo   lint     Run staticcheck
	@echo   test     Fast tests (cached, skips fsnotify integration tests)
	@echo   test-all Full test suite (cached)
	@echo   retest   Force a full re-run, bypassing the cache
	@echo   clean    Remove build artifacts
	@echo   docker-build  Build docker image
	@echo   docker-run    Run docker image
	@echo   docker-clean  Remove docker image

all: build
