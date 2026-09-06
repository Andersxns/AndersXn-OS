# AndersXn OS - top-level build entry points.
#
#   make iso              amd64 live installer ISO
#   make iso ARCH=arm64   arm64 live installer ISO
#   make image ARCH=arm64 arm64 ready-to-flash raw disk image
#   make installer        build ax-installer for the host, into dist/
#   make check            syntax, vet and unit tests - no root needed
#   make clean            remove work/ for the selected architecture

SHELL := /usr/bin/env bash
ARCH  ?= amd64
BOOTLOADER ?= limine

BUILD := ./build/build.sh
ROOT  := $(CURDIR)

.PHONY: all iso image installer ascii check check-shell check-go check-modules \
        test fmt vet clean distclean help

all: iso

## --- images ---------------------------------------------------------------

iso:
	sudo $(BUILD) --arch $(ARCH) --bootloader $(BOOTLOADER)

image:
	sudo AXOS_BUILD_RAW=1 $(BUILD) --arch $(ARCH) --bootloader $(BOOTLOADER)

# Re-run a single stage, e.g.: make stage N=30
stage:
	@test -n "$(N)" || { echo "usage: make stage N=<number>"; exit 1; }
	sudo $(BUILD) --arch $(ARCH) --only $(N)

## --- installer ------------------------------------------------------------

installer: ascii
	cd installer && CGO_ENABLED=0 go build -trimpath \
		-ldflags "-s -w" \
		-o $(ROOT)/dist/ax-installer ./cmd/ax-installer
	@echo "built dist/ax-installer"

# Regenerate the compact and mini ASCII marks from the master art.
ascii:
	python3 branding/ascii/generate-variants.py
	@for v in axos-logo.txt axos-logo-compact.txt axos-logo-mini.txt; do \
		cp branding/ascii/$$v installer/internal/branding/assets/$$v; \
	done
	@echo "ASCII marks regenerated and synced into the installer"

## --- checks ---------------------------------------------------------------

check: check-shell check-go check-modules test

check-shell:
	@echo "==> shell syntax"
	@fail=0; \
	for f in build/build.sh build/stages/*.sh build/lib/*.sh \
	         provisioning/postinstall.sh provisioning/modules/*.sh \
	         provisioning/bin/* branding/etc/update-motd.d/*; do \
		bash -n "$$f" || { echo "   FAIL $$f"; fail=1; }; \
	done; \
	exit $$fail
	@command -v shellcheck >/dev/null 2>&1 && { \
		echo "==> shellcheck"; \
		shellcheck -S warning build/build.sh build/stages/*.sh build/lib/*.sh \
			provisioning/postinstall.sh provisioning/modules/*.sh || true; \
	} || echo "   (shellcheck not installed - skipped)"

check-go:
	@echo "==> gofmt"
	@test -z "$$(cd installer && gofmt -l .)" || { \
		echo "   these files need gofmt:"; cd installer && gofmt -l .; exit 1; }
	@echo "==> go vet"
	cd installer && go vet ./...

# The Go catalog and the shell modules are two halves of one contract; a
# mismatch means the installer offers something it cannot deploy.
check-modules:
	@echo "==> catalog/module parity"
	@fail=0; \
	for id in $$(grep -oP '^\s*ID:\s*"\K[a-z0-9-]+' installer/internal/provision/catalog.go); do \
		ls provisioning/modules/[0-9][0-9]-$$id.sh >/dev/null 2>&1 || { \
			echo "   catalog module '$$id' has no provisioning script"; fail=1; }; \
	done; \
	for f in provisioning/modules/[0-9][0-9]-*.sh; do \
		id=$$(basename "$$f" .sh | sed 's/^[0-9][0-9]-//'); \
		grep -q "ID:.*\"$$id\"" installer/internal/provision/catalog.go || { \
			echo "   module '$$id' is not in the installer catalog"; fail=1; }; \
	done; \
	test $$fail -eq 0 && echo "   catalog and modules agree"; \
	exit $$fail

test:
	@echo "==> go test"
	cd installer && go test ./...

fmt:
	cd installer && gofmt -w .

vet:
	cd installer && go vet ./...

## --- housekeeping ---------------------------------------------------------

clean:
	sudo $(BUILD) --arch $(ARCH) --clean

distclean: clean
	rm -rf dist work

help:
	@echo "AndersXn OS build targets:"
	@sed -n 's/^## --- \(.*\) ---.*/\n\1:/p; s/^\([a-z][a-z-]*\):.*/  \1/p' $(MAKEFILE_LIST)
