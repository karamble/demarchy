# Demarchy FAQ

## Why does Demarchy build from source?

The plugin ships QML and Go source, not binaries. `bin/` is not committed, so
the panel helper and `demarchy-setup` are compiled once on the machine that
runs them. Building needs the Go toolchain.

## The build says the Go toolchain is not on PATH

The preflight tells you which of three situations you are in. This page has the
commands for each.

### mise has Go, but this shell cannot see it

mise activates per shell, so a shell opened before you installed Go does not
have it. Open a new terminal and press Build again, or point the build straight
at the toolchain mise already has:

```
make GO=$(mise which go)
```

### mise is installed, but has no Go

Omarchy ships mise, and it needs no root:

```
mise use -g go@latest
```

Then open a new terminal and press Build again.

### No mise, no Go

Install Go with the system package manager:

```
sudo pacman -S go
```

Or install mise first and use the previous answer, which keeps toolchains in
your home directory instead of system wide.

## Which Go version?

The minimum is the version in `go.mod`. The preflight prints it.

## The panel says Demarchy is not built yet

The QML is installed but the binaries are missing. Run `make` in the plugin
directory, then restart the shell.

## Where does Demarchy get its data?

From a dcrpulse you already run, over a read-only MCP connection. The setup
tool refuses a token that is not read-only. Connections live in
`~/.config/demarchy/connections.json`, mode 0600, and are switchable from the
panel. Demarchy reaches nothing else.
