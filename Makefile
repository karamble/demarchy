PLUGIN_ID := karamble.demarchy
DEST      := $(HOME)/.config/omarchy/plugins/$(PLUGIN_ID)
GO        ?= go

# The marketplace shows the manifest's version. The guard fails when this and
# manifest.json disagree.
VERSION ?= 0.2.0

.PHONY: all build install lint test clean toolchain


all: build

# Both binaries together: a connections.json carrying a plain-http exception is
# written as version 2, and a helper older than the wizard refuses to read it.
build: toolchain
	$(GO) build -trimpath -o bin/demarchy       ./cmd/demarchy
	$(GO) build -trimpath -o bin/demarchy-setup ./cmd/demarchy-setup

# The FAQ the toolchain preflight points at. Install instructions belong in
# documentation a person can read and correct, not in a build target.
FAQ_URL := https://github.com/karamble/demarchy/blob/master/docs/FAQ.md

# Building from source needs Go, and the most common way to miss it on Omarchy
# is having it under mise without the shell activated. Say which case it is.
toolchain:
	@command -v $(GO) >/dev/null 2>&1 && exit 0; \
	echo "Demarchy builds from source and the Go toolchain is not on PATH."; \
	echo; \
	if command -v mise >/dev/null 2>&1 && mise which go >/dev/null 2>&1; then \
		echo "  mise has Go, but this shell cannot see it. Open a new terminal and"; \
		echo "  press Build again, or build against it directly:"; \
		echo; \
		echo "      make GO=$$(mise which go)"; \
	elif command -v mise >/dev/null 2>&1; then \
		echo "  Omarchy ships mise, so the shortest way is:"; \
		echo; \
		echo "      mise use -g go@latest"; \
		echo; \
		echo "  then open a new terminal and press Build again."; \
	else \
		echo "  Installing Go, or keeping toolchains in your home directory:"; \
		echo; \
		echo "      $(FAQ_URL)"; \
	fi; \
	echo; \
	echo "Go $(shell sed -n 's/^go \([0-9.]*\)$$/\1/p' go.mod) or newer is needed."; \
	exit 1

# rsync rather than a symlink: the shell rejects symlinks anywhere inside a
# plugin directory.
install: build
	mkdir -p $(DEST)
	rsync -a --delete --exclude .git --exclude cmd --exclude internal \
	      --exclude go.mod --exclude go.sum --exclude Makefile --exclude '*.go' ./ $(DEST)/
	omarchy-shell -q shell rescanPlugins || true

# The qmllint and qmlformat on PATH may not be Qt 6's: some distributions ship
# an unrelated binary of the same name that reports version 1.0 and fails on
# `pragma ComponentBehavior: Bound` with no output at all. Prefer Qt's own.
QMLLINT   := $(shell command -v qmllint6 2>/dev/null || echo /usr/lib/qt6/bin/qmllint)
QMLFORMAT := $(shell command -v qmlformat6 2>/dev/null || echo /usr/lib/qt6/bin/qmlformat)
SHELL_DIR := $(or $(OMARCHY_PATH),/usr/share/omarchy)/shell
LINTROOT  := $(CURDIR)/.lintroot

lint: test
	go vet ./...
	@unformatted=$$(gofmt -l .); 	  test -z "$$unformatted" || { echo "gofmt needed on:"; echo "$$unformatted"; exit 1; }
	@for f in *.qml; do $(QMLFORMAT) "$$f" >/dev/null || { echo "failed to parse $$f"; exit 1; }; done
	@echo "qml: all files parse"
	@# `import qs.Ui` resolves as <import path>/qs/Ui/qmldir, so the shell has
	@# to be reachable under a directory named `qs`.
	@mkdir -p $(LINTROOT) && ln -sfn $(SHELL_DIR) $(LINTROOT)/qs
	$(QMLLINT) -I $(LINTROOT) *.qml
	@rm -rf $(LINTROOT)
	omarchy plugin validate $(DEST)

test:
	go test ./...

clean:
	rm -rf bin .lintroot
