# Go port — profile and library evidence

*Supporting file for [`design.md`](design.md) § Profile format, § CLI and
§ Global Constraints. The library probes ran on macOS arm64 with go1.26.7
and hyperfine 1.20.0, from nixpkgs.*

## Profile as JSON

These facts were checked on 2026-10-10.

- **Go 1.27.** `encoding/json/v2` is GA in Go 1.27. On Go 1.26 it builds
  only with `GOEXPERIMENT=jsonv2`, so `go.mod` declares `go 1.27`.
- **Unknown members.** The loader decodes with:

  ```go
  json.Unmarshal(data, &p, json.RejectUnknownMembers(true))
  ```

  A member with no Go field fails the decode. A Go field with no member
  keeps its zero value, so `Validate` checks the required fields.
- **Zod 4.** Parsed output keeps the schema's key order, so a render is
  byte-stable. `z.toJSONSchema` emits a JSON Schema from a Zod schema.
- **tsc and callbacks.** `tsc` does not report an extra, mistyped key in an
  object literal that a callback returns. For example:

  ```ts
  // tsc accepts the mistyped key "wieghts" in the callback's literal.
  const desk: DeskLayout[] = names.map((name) => ({ ...stack(name), wieghts: [1] }));
  ```

  So the render must reject unknown keys at run time. A `z.strictObject`
  does that.

## Library evidence

Collected on 2026-10-06. "Importers" is the "Known importers" count on the
package's pkg.go.dev `?tab=importedby` page. "Latest" is
`proxy.golang.org/<module>/@latest`. "Last commit" is the head of the
default branch from the GitHub API. No repository listed is archived.

| Package | Importers | Latest | Released | Last commit |
| --- | ---: | --- | --- | --- |
| `github.com/spf13/cobra` | 195,884 | v1.10.2 | 2025-12-03 | 2026-07-11 `adbc881` |
| `github.com/urfave/cli/v3` | 4,439 | v3.14.0 | 2026-10-02 | 2026-10-02 `2f64589` |
| `github.com/alecthomas/kong` | 3,079 | v1.16.1 | 2026-08-09 | 2026-08-28 `a60008c` |
| `github.com/willabides/kongplete` | 48 | v0.4.0 | 2023-11-15 | 2023-11-15 `b59327a` |
| `github.com/gofrs/flock` | 1,499 | v0.13.1 | 2026-08-27 | 2026-10-01 `844daec` |
| `github.com/bep/debounce` | 277 | v1.2.1 | 2022-05-15 | 2026-08-21 `0ed0c00` |
| `github.com/kardianos/service` | 1,591 | v1.3.0 | 2026-07-06 | 2026-08-29 `9907089` |
| `howett.net/plist` | 543 | v1.0.1 | 2023-10-24 | 2026-08-19 `760f9a5` |
| `github.com/adrg/xdg` | 1,197 | v0.5.3 | 2024-10-31 | 2026-09-15 `424b3a2` |
| `golang.org/x/sync/errgroup` | 30,110 | v0.23.0 | 2026-08-31 | 2026-09-23 `36f2d70` |
| `github.com/samber/lo` | 12,533 | v1.53.0 | 2026-03-02 | 2026-10-01 `5c6ddcb` |
| `github.com/samber/mo` | 434 | v1.17.0 | 2026-06-02 | 2026-10-01 `502998f` |
| `github.com/samber/oops` | 332 | v1.23.2 | 2026-09-14 | 2026-10-01 `cf12269` |
| `github.com/deckarep/golang-set/v2` | 1,842 | v2.9.0 | 2026-04-21 | 2026-10-02 `20c6d8d` |
| `github.com/stretchr/testify/require` | 20,797 | v1.12.1 | 2026-08-17 | 2026-09-24 `87a7b9d` |
| `github.com/google/go-cmp/cmp` | 5,704 | v0.7.0 | 2025-01-14 | 2026-06-18 `b133f1f` |
| `gotest.tools/v3/assert` | 1,570 | v3.5.2 | 2024-09-05 | 2026-09-14 `749748e` |
| `gotest.tools/v3/golden` | 6 | v3.5.2 | 2024-09-05 | 2026-09-14 `749748e` |

**Test imports are not counted.** pkgsite builds a package's import list
from its documentation, and skips test files when it loads the package.
[golang/pkgsite `internal/fetch/load.go` at 16c0943](https://github.com/golang/pkgsite/blob/16c0943645e1dd7e529ec49dbfcee2a085989734/internal/fetch/load.go),
symbol `loadFilesWithBuildContext`:

```go
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
```

So `gotest.tools/v3/golden` shows 6 importers although docker/cli uses it
in 63 test files. The bar measures a test-only module by its most-imported
package, `gotest.tools/v3/assert`.

**Named users.** Each `go.mod` (or docker/cli's `vendor.mod`) was read at
the default-branch head on 2026-10-06:

| Module | Projects that pin it (version) |
| --- | --- |
| cobra | kubernetes, cli/cli, hugo, helm, golangci-lint, grafana, docker/buildx (v1.10.2) |
| urfave/cli | gitea (v3.13.0); go-ethereum and grafana (v2) |
| kong | hermit (v1.16.1), block/ftl (v1.11.0) |
| gofrs/flock | helm, golangci-lint, docker/buildx, moby, traefik (v0.13.1); prometheus (v0.13.0); go-ethereum (v0.12.1) |
| samber/lo | terraform, lazygit, traefik (v1.53.0) |
| samber/oops | aquasecurity/trivy-db |
| golang-set | go-ethereum (v2.6.0) |
| gotest.tools/v3 | docker/cli, moby, cli/cli (v3.5.2) |
| x/sync | kubernetes, cli/cli, hugo, helm, golangci-lint (v0.23.0) |
| go-cmp | kubernetes, cli/cli, helm, prometheus (v0.7.0) |

## CLI library

Each library got the same probe: 21 subcommands built from one data
table, with `snap` taking one of `3col`, `50-50` or `columns`, and an
injected `context.Context`. The binaries ran on 2026-10-06.

| Case | cobra v1.10.2 | urfave/cli v3.14.0 | kong v1.16.1 |
| --- | --- | --- | --- |
| `tess bogus` | rc 1, `unknown command "bogus" for "tess"`, "Did you mean this? focus" | rc 3, `No help topic for 'bogus'` | rc 1, `unexpected argument bogus, did you mean "focus"?` |
| bare `tess` | rc 0, help | rc 0, help | rc 1 |
| `tess snap 9col` | rc 1, `invalid argument "9col" for "tess snap"` | rc 1, from a hand-written check | rc 1, `<mode> must be one of …` |
| completion of `snap` | `3col`, `50-50`, `columns` | `help` only | none built in |
| built from the data table | yes | yes | no, struct tags |

cobra output:

```text
$ tess bogus
tess: unknown command "bogus" for "tess"

Did you mean this?
	focus

usage: tess
rc=1
$ tess snap 9col
tess: invalid argument "9col" for "tess snap"
usage: tess snap {3col|50-50|columns} [flags]
rc=1
$ tess __complete snap ""
3col
50-50
columns
:4
Completion ended with directive: ShellCompDirectiveNoFileComp
```

The unknown-command error comes from cobra's default `Args` for a root
with subcommands.
[spf13/cobra `args.go` at v1.10.2](https://github.com/spf13/cobra/blob/v1.10.2/args.go),
symbol `legacyArgs`:

```go
	// root command with subcommands, do subcommand checking.
	if !cmd.HasParent() && len(args) > 0 {
		return fmt.Errorf("unknown command %q for %q%s", args[0], cmd.CommandPath(), cmd.findSuggestions(args[0]))
	}
```

A nil argv falls back to the process arguments.
[`command.go` at v1.10.2](https://github.com/spf13/cobra/blob/v1.10.2/command.go),
symbol `ExecuteC`:

```go
	if c.args == nil && filepath.Base(os.Args[0]) != "cobra.test" {
		args = os.Args[1:]
	}
```

urfave/cli exits 3 on an unknown command unless `CommandNotFound` is set.
[urfave/cli `help.go` at v3.14.0](https://github.com/urfave/cli/blob/v3.14.0/help.go),
symbol `DefaultShowCommandHelp`:

```go
		tracef("exiting 3 with errMsg %[1]q", errMsg)
		return Exit(errMsg, 3)
```

kong has no built-in completion. The `kongplete` add-on has 48 importers
and no release since 2023-11-15 (table above).

The nixpkgs `gh` package installs cobra's completion scripts. From
[NixOS/nixpkgs `pkgs/by-name/gh/gh/package.nix` at 1c057c7](https://github.com/NixOS/nixpkgs/blob/1c057c7b9b623caae4a69c6e0d0c9d83f593311e/pkgs/by-name/gh/gh/package.nix):

```nix
    installShellCompletion --cmd gh \
      --bash <($out/bin/gh completion -s bash) \
      --fish <($out/bin/gh completion -s fish) \
      --zsh <($out/bin/gh completion -s zsh)
```

**Process start.** hyperfine `-N`, 500 runs each; p50 and p90 from the
exported JSON:

| Binary | p50 / p90 |
| --- | --- |
| empty Go `main` | 2.82 / 3.28 ms |
| cobra `snap 3col` | 3.72 / 5.75 ms |
| urfave/cli `snap 3col` | 3.80 / 5.14 ms |
| kong `snap 3col` | 4.09 / 5.79 ms |
| `samber/lo` linked | 3.12 / 3.52 ms |

cobra adds about 0.9 ms at p50.

## Locks

`gofrs/flock` v0.13.1 probe, two handles on one path in one process:

```text
first TryLock: true <nil>
second handle, same process: false <nil>
unlock first: <nil>
second after release: true <nil>
lock file still present after unlock: true
unlock second: <nil>
```

[gofrs/flock `flock.go` at v0.13.1](https://github.com/gofrs/flock/blob/v0.13.1/flock.go),
symbol `Close`:

```go
// This will release the lock and close the underlying file descriptor.
// It will not remove the file from disk, that's up to your application.
```

## Kept hand-rolled

- **Debounce.** `bep/debounce` is the most-imported Go debouncer, at 277
  importers. Its whole API is `func New(after time.Duration) func(f
  func())`: a trailing timer, with no quiet-window gate across channels and
  no restamp.
- **launchd calls.** `kardianos/service` v1.3.0 calls the legacy verbs.
  [`service_darwin.go` at v1.3.0](https://github.com/kardianos/service/blob/v1.3.0/service_darwin.go):

  ```go
  	return run("launchctl", "load", confPath)
  	...
  	return run("launchctl", "unload", confPath)
  ```

  tess needs `print`, `enable`, `bootstrap`, `kickstart` and `bootout` on
  plists that yabai and skhd install.
- **XDG paths.** `adrg/xdg` v0.5.3 defaults to `~/Library` paths on macOS.
  [`paths_darwin.go` at v0.5.3](https://github.com/adrg/xdg/blob/v0.5.3/paths_darwin.go):

  ```go
  	baseDirs.configHome = pathutil.EnvPath(envConfigHome, homeAppSupport)
  	...
  	baseDirs.cacheHome = pathutil.EnvPath(envCacheHome, filepath.Join(home, "Library", "Caches"))
  ```

- **Sets.** `golang-set` v2.9.0 adds BSON methods to its `Set` interface,
  so its module requires `go.mongodb.org/mongo-driver` v1.17.4.
  [`set.go` at v2.9.0](https://github.com/deckarep/golang-set/blob/v2.9.0/set.go):

  ```go
  import "go.mongodb.org/mongo-driver/bson/bsontype"
  ```

  The engine needs add, has, clone and equal, which a `map[T]struct{}` with
  `maps.Clone` and `maps.Equal` gives directly.
