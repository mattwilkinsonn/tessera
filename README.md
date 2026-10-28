# tessera

Write your tiling window manager layout in a CUE profile, validated in-process by `tess`.

Tessera is a config layer for [yabai](https://github.com/koekeishiya/yabai) on
macOS. You describe your monitors, your windows, and the exact column layout you
want on each screen in one typed file. Tessera converges the live window tree
onto that layout: it places windows into columns, stacks the overflow, keeps
splits at fixed ratios, and rebuilds the arrangement when a display is plugged
or unplugged.

The layout is data, not a script. A single `Profile` object binds windows to
logical monitors by width, so the same config drives a three-monitor desk and a
laptop-only setup without an edit — a monitor that is absent is skipped rather
than special-cased.

## Status

- macOS via yabai: working.
- The Go CLI and layout engine are built into one `tess` binary.

## How it works

Tessera ships as one binary, `tess`, invoked per action. Your yabai signals and
your hotkey daemon (skhd, or any launcher) call `tess <subcommand>`; each
invocation reads the live window state, computes the target layout, and issues
the yabai commands to reach it.

```text
tess apply                      converge the active space onto its configured layout
tess laptop                     converge the laptop-only fallback layout
tess rules                      apply window rules from the profile
tess display-setup              set up displays for the current profile
tess focus-slot N               focus a named slot
tess snap MODE                  snap the focused window to a layout mode
tess stack-cycle DIR            cycle the focused stack
tess resize DIR                 resize the active column
tess move-display g9|aw|laptop  move the focused window to a display
tess cycle-display next|prev    move the focused window to a neighboring display
tess reset-splits               reset splits in the active space
tess columns                    re-flow the active space into its columns
tess focus DIR                  focus the neighboring window
tess swap DIR                   swap the focused window with its neighbor
tess warp DIR                   warp the focused window to its neighbor
tess insert DIR                 set the insertion direction for the focused window
tess toggle-float               toggle floating for the focused window
tess balance                    balance the active space
tess space LAYOUT               apply a named space layout
tess daemon                     run the resident event daemon
tess wake                       ask the resident daemon to run its wake routine
```

Run `tess --help` (or `tess -h`) for the generated subcommand help. Run
`tess --version` (or `tess -v`) for the version. Bare `tess` also prints the
help.

### Exit codes

- `0` — success, help/version output, or bare `tess` displaying help.
- `1` — invalid command or arguments (with help and an error), profile or
  driver error, or a contended `laptop` command.

## Architecture

The Go implementation splits into packages, each with a single responsibility:

- `internal/config/` — profile types, CUE schema, validation, and the embedded CUE default.
- `internal/executor/` — applies engine plans through the window-manager driver.
- `internal/commands/` — composes the engine, executor, effects, and driver into each subcommand.
- `internal/wm/` — the window-manager boundary and yabai driver.
- `internal/effects/` — locks, the signal guard, and state stamps around each invocation.
- `internal/daemon/` — the resident daemon: socket, event debounce, signal wiring, and launchd bootstrap.
- `internal/cli/` — argument parsing and dispatch to commands or the daemon.

The engine remains pure, so layout behavior can be tested without spawning yabai.

## Configuration

The binary loads a CUE `Profile` at startup, resolved in this order:

1. `$TESSERA_PROFILE` — an explicit profile path (overrides everything below). CUE files and JSON are accepted.
2. `${XDG_CONFIG_HOME:-~/.config}/tessera/profile.cue` — the well-known user profile path.
3. The embedded `default.cue` profile.

A missing well-known `profile.cue` falls back to the embedded default. A present but broken profile is an error. If `profile.cue` is absent while a legacy `profile.ts` or `profile.json` exists, `tess` reports that file and directs you to use `profile.cue`. A `.ts` path in `TESSERA_PROFILE` is also rejected.

The schema is installed at `share/tessera/schema.cue` alongside the `tess` binary. Validate a profile against the schema with CUE's CLI:

```sh
schema="$(dirname "$(dirname "$(command -v tess)")")/share/tessera/schema.cue"
cue export "${XDG_CONFIG_HOME:-$HOME/.config}/tessera/profile.cue" --out json |
  cue vet -c -d '#Profile' "$schema" json: -
```

When `TESSERA_PROFILE` and the well-known profile are absent, `tess` loads its embedded `default.cue` profile.

The CLI and engine are written in Go. The profile schema uses CUE and is also embedded in the binary.

```sh
direnv exec . gofmt -l .
direnv exec . go vet ./...
direnv exec . go mod tidy -diff
direnv exec . golangci-lint run --config .golangci.yml ./...
direnv exec . nilaway -include-pkgs=github.com/mattwilkinsonn/tessera -test=false ./...
direnv exec . go build ./...
direnv exec . go test -race ./...
direnv exec . govulncheck ./...
direnv exec . sh -c "cue export internal/config/default.cue --out json | cue vet -c -d '#Profile' internal/config/schema.cue json: -"
direnv exec . actionlint .github/workflows/*.yml
nix build .#default
nix flake check
```

`devenv.nix` provides Go 1.27.3 from go-overlay, the CUE CLI, pinned Go analysis tools built with that toolchain, and documentation/workflow linters. With [direnv](https://direnv.net/) installed, `direnv allow` loads the shell on `cd`. CI builds its Go analysis tools with the same Go version and runs the checks plus the Nix build in `.github/workflows/ci.yml`.

## License

MIT. See [LICENSE](LICENSE).
