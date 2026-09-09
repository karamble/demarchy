PLUGIN_ID := karamble.demarchy
DEST      := $(HOME)/.config/omarchy/plugins/$(PLUGIN_ID)

.PHONY: all build install lint test clean


all: build

# Both binaries together: a connections.json carrying a plain-http exception is
# written as version 2, and a helper older than the wizard refuses to read it.
build:
	go build -trimpath -o bin/demarchy       ./cmd/demarchy
	go build -trimpath -o bin/demarchy-setup ./cmd/demarchy-setup

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
