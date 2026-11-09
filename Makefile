# Build steps adapted from Deputy's Makefile (itself from Notted and github.com/brienze1/quant, MIT); see NOTICE.md.
export PATH := $(HOME)/.gvm/pkgsets/go1.26.5/global/bin:$(HOME)/.gvm/gos/go1.26.5/bin:$(HOME)/.nvm/versions/node/v24.16.0/bin:$(PATH)
.PHONY: dev build release test test-go test-fe
APP := build/bin/Hadron.app
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

dev:
	wails dev

# VERSION is baked into the binary: make build VERSION=v0.0.1-test
build:
	MACOSX_DEPLOYMENT_TARGET=14.0 CGO_CFLAGS=-mmacosx-version-min=14.0 CGO_LDFLAGS=-mmacosx-version-min=14.0 wails build -platform darwin/arm64 -ldflags "-X github.com/GabiHert/hadron/internal/run.Version=$(VERSION)"
	touch frontend/dist/.gitkeep
	bash scripts/sign-bundle.sh $(APP)
	bash scripts/check-bundle.sh $(APP)

# make release builds Hadron, tags it and publishes a GitHub Release (see scripts/release.sh).
# make release V=v0.1.0 picks the version; without V it is the next patch after the newest tag.
release:
	bash scripts/release.sh $(V)

test-go:
	go vet ./...
	go test -race ./...

test-fe:
	npm --prefix frontend ci
	npm --prefix frontend run typecheck
	npm --prefix frontend test

test: test-go test-fe
