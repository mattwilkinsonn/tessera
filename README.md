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

Each layout remembers which window filled each slot, per display and mode, in
`$XDG_CACHE_HOME/yabai/window-slots.json`, so switching modes or monitors puts
windows back. It survives tess and daemon restarts. An app restart gives its
windows new ids, so the next layout relearns them. Delete the file to reset.

Spawn demand counts matching app and title windows on every space, including
floating and minimized windows. Title-specific slots claim before app-only slots.
`spawn-ledger.json`, next to `window-slots.json`, records a
JSON object from window-spec names to arrays of RFC 3339 timestamps. The shared
slot lock protects it. Each name has a five-minute launch cooldown, including
launches whose windows never appear. Spawns wait until yabai has been up for
30 seconds and stop after a query failure. A held wake arms one cancellable
timer that sends a fresh wake through the daemon socket without blocking the
current wake. Stack insertions stop at 24 windows; rejected
arrivals remain floating.

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

On a desk setup, `tess apply` adopts newly launched browser windows into configured
slots. Eligibility polling has a five-second budget and does not mutate layouts.
Apply then rebuilds the desk once, with at most one additional rebuild for windows
or owners that changed during the first. Layout execution uses the caller's context,
not the polling budget. Selected slot owners never become overflow. Existing
floating windows are left alone. Owners must be tiled on the configured desk home
space before their slots are recorded.

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

The daemon socket and apply/laptop locks use the per-user Darwin temp directory,
resolved with `/usr/bin/getconf DARWIN_USER_TEMP_DIR`. This keeps launchd and
shell processes on the same paths regardless of `TMPDIR`. On other platforms,
or when the lookup fails, Tessera uses the process temp directory.

At daemon startup, Tessera waits up to 60 seconds for yabai to answer a display
query before running the initial wake cascade. If yabai is still unavailable,
the daemon keeps serving; a later `tess wake` can retry the cascade.

On a desk, newly created tile-type windows are floated by yabai and placed by
the resident daemon. The laptop home stack remains the target when the laptop
display is connected. With the laptop absent, arrivals use AW's left slot when
AW is connected, or G9's left slot otherwise. When the target slot has an
eligible tiled window, Tessera stacks the arrival onto the leftmost such window.
Placement waits for display changes to settle and shares serialization with wake
and display cascades. Laptop-only arrival rules and flex convergence remain
unchanged. A desk-to-laptop transition hands off surviving floating arrivals
before laptop convergence.

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
