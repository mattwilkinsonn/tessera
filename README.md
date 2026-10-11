# tessera

Write your tiling window manager layout in a typed TypeScript profile, rendered to JSON.

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
tess apply            converge the active space onto its configured layout
tess laptop           converge the laptop-only fallback layout
tess rules            apply window rules from the profile
tess display-setup    set up displays for the current profile
tess focus-slot N     focus a named slot
tess snap MODE        snap the focused window to a layout mode
tess stack-cycle DIR  cycle the focused stack
tess resize DIR       resize the active column
tess move-display N   move the focused window to a display
tess cycle-display D  move the focused window to a neighboring display
tess reset-splits     reset splits in the active space
tess columns          re-flow the active space into its columns
tess focus DIR        focus the neighboring window
tess swap DIR         swap the focused window with its neighbor
tess warp DIR         warp the focused window to its neighbor
tess insert DIR       set the insertion direction for the focused window
tess toggle-float     toggle floating for the focused window
tess balance          balance the active space
tess space LAYOUT     apply a named space layout
tess daemon           run the resident event daemon
tess wake             ask the resident daemon to run its wake routine
```

Run `tess --help` (or `tess -h`) for the generated subcommand help. Run
`tess --version` (or `tess -v`) for the version. Bare `tess` also prints the
help.

### Exit codes

- `0` — success, help/version output, or bare `tess` displaying help.
- `1` — invalid command or arguments (with help and an error), profile or
  driver error, or a contended `laptop` command.

## Architecture

The Go implementation has four layers, each with a single responsibility:

- `internal/config/` — profile types, validation, and the embedded JSON default.
- `internal/engine/` — pure layout planning from the current window world and a profile.
- `internal/wm/` — the window-manager boundary and yabai driver.
- `internal/effects/` — locks, debounce, and state stamps around each invocation.

The CLI wires the driver and profile to the commands. The engine remains pure, so layout behavior can be tested without spawning yabai.

## Configuration

The binary loads a JSON `Profile` at startup, resolved in this order:

1. `$TESSERA_PROFILE` — an explicit path to a JSON profile (overrides everything below).
2. `${XDG_CONFIG_HOME:-~/.config}/tessera/profile.json` — the well-known user profile path.
3. The embedded default JSON profile — a neutral example used when neither of the above resolves.

Install the [`tessera-wm`](schema/) package in your profile project. Import `Profile` and render `Profile.parse(profile)` to `~/.config/tessera/profile.json` during your build. `tess` reads JSON and does not run TypeScript.

A missing well-known JSON file falls back to the embedded default. A present but broken JSON profile surfaces its error. A legacy `profile.ts` at the well-known location, or a `.ts` path in `TESSERA_PROFILE`, fails with an instruction to render the profile to JSON.

The CLI and engine are written in Go. The schema package uses TypeScript and Bun; `tsc` and Bun run its checks.

```sh
direnv exec . go vet ./...
direnv exec . golangci-lint run ./...
direnv exec . go test -race ./...

cd schema
bunx tsc --noEmit
bun test
```

`devenv.nix` provides Go 1.27, `golangci-lint`, Bun, and the documentation and workflow linters. With [direnv](https://direnv.net/) installed, `direnv allow` loads the shell on `cd`. CI runs the Go checks and Nix build in `.github/workflows/ci.yml`; `.github/workflows/schema.yml` checks the schema package.

## License

MIT. See [LICENSE](LICENSE).
