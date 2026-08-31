# tessera

Write your tiling window manager layout in TypeScript.

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
- The driver boundary (`src/driver/`) isolates every yabai call behind one
  interface, so a second backend (e.g. Hyprland on Linux) is a new driver, not a
  rewrite of the engine. That backend is not built yet.

## How it works

Tessera ships as one binary, `tess`, invoked per action. Your yabai signals and
your hotkey daemon (skhd, or any launcher) call `tess <subcommand>`; each
invocation reads the live window state, computes the target layout, and issues
the yabai commands to reach it.

```text
tess apply            converge the active space onto its configured layout
tess laptop           converge the laptop-only fallback layout
tess display-event    rebuild after a display is added or removed
tess flex-event       reflow when a flex space changes
tess init             register the yabai signals and run the startup cascade
tess focus DIR        focus the neighboring window
tess focus-slot N     focus a named slot (e.g. a numpad binding)
tess snap MODE        snap the focused window to a layout mode
tess columns          re-flow the active space into its columns
tess stack-cycle DIR  cycle the focused stack
tess resize DIR       resize the active column
tess move-display N   move the focused window to a display
tess space LAYOUT     apply a named space layout
```

Run `tess` with no arguments for the full subcommand list.

## Architecture

Four layers, each with a single responsibility:

- `config/` — the typed layout shapes and your `profile.ts` values. Pure data.
- `engine/` — pure planning. Given the current window world and a profile, it
  computes what should change. No I/O, fully unit-tested.
- `driver/` — the only code that talks to yabai. One interface, one real
  implementation, one fake for tests.
- `effects/` — the runtime concerns around a single invocation: locks so
  concurrent signals do not fight, debounce so a burst of display events
  collapses into one rebuild, and state stamps.

`index.ts` is a thin router: it parses `argv`, wires the real driver and your
profile, and dispatches to a command. Because the engine is pure and the driver
is swappable, the whole layout logic runs in tests without spawning yabai.

## Configuration

You edit one file: `src/config/profile.ts`. It defines your displays (keyed by
width in pixels, which is stable across reconnects where the macOS display index
is not), your windows (matched by app and title regex), and the column layout
for each space on each display. The shapes it must satisfy live in
`src/config/types.ts`.

`src/config/profile.example.ts` is a fully annotated starting point — a neutral
two-monitor setup with placeholder apps. Copy it to `profile.ts` and edit it for
your own machine.

## Development

Tessera is a single zero-dependency Bun project. The toolchain is Bun for the
runtime, test runner, and bundler; [Biome](https://biomejs.dev/) for lint and
format; `tsc` for type checking.

```sh
bun install
bun run typecheck   # tsc --noEmit
bun run check       # biome check src/
bun test            # the full suite
bun run build       # compile the tess binary
```

A [devenv](https://devenv.sh/) shell (`devenv.nix`) provides Bun and the
linters; with [direnv](https://direnv.net/) installed, `direnv allow` loads it
on `cd`. CI (`.github/workflows/ci.yml`) runs the same three checks on every
pull request.

## License

MIT. See [LICENSE](LICENSE).
