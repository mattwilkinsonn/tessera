# Go port — profile language and library evidence

*Supporting file for [`design.md`](design.md) § Profile format, § CLI and
§ Global Constraints. All probes ran on macOS arm64 with cue v0.17.1,
`cuelang.org/go` v0.17.1, go1.26.7, pkl 0.31.1 and hyperfine 1.20.0, from
nixpkgs.*

## CUE expresses the profile rules

`cue vet -c` against a schema with the cross-field rules gave 12 correct
verdicts out of 12:

```text
ok accept: good-layout
ok reject: weights-len :: x.weights: invalid value [1,2,3] (does not satisfy list.MaxItems(2)): len(list) > MaxItems(2) (3 > 2):
ok reject: one-track-weights :: explicit error (_|_ literal) in source:
ok reject: stack-weights :: explicit error (_|_ literal) in source:
ok accept: good-table
ok reject: key-one :: x.columns."1": field not allowed:
ok reject: key-text :: x.rows.two: field not allowed:
ok accept: good-topology
ok reject: topo-missing :: x._check: incompatible list lengths (1 and 2)
ok reject: topo-undeclared :: x._check: incompatible list lengths (1 and 2)
ok reject: topo-duplicate :: x._laidOut: invalid value ["g9","g9"] (does not satisfy list.UniqueItems):
ok reject: bad-regex :: x.app: invalid value "(?<=x)y" (does not satisfy regexp.Valid): error parsing regexp: invalid named capture: `(?<=x)y`:
```

The Go loader shape (`cue.Scope(schema)`, unify with `#Profile`, validate
concrete, `Decode`) on a bad and a good file:

```text
ERROR profile load failed err="profile bad.cue: #Profile.laptopStakApps.0: field not allowed:\n    bad.cue:5:18\n#Profile.windows.editor.app: invalid value \"(?<=x)\" (does not satisfy regexp.Valid): error parsing regexp: invalid named capture: `(?<=x)`:\n    bad.cue:3:16\n    schema.cue:12:10\n"
exit=1
loaded in 909.5µs: browser app="Browser" matches=true weights.columns[3]=[] stack=[Chat]
exit=0
```

`Decode` fills Go types directly: `columns[3]=[3 4 3] arc="^Arc$"
matches(Arc)=true`.

`cue vet -c user/<file> schema.cue` with no `cue.mod`:

```text
-- good.cue
accepted
-- bad.cue
laptopStackApps: field is required but not present:
windows.arc.app: field is required but not present:
-- rendered.json
accepted
```

A `CGO_ENABLED=0` build links only libSystem, libresolv, CoreFoundation and
Security.

## Process start

hyperfine `-N --warmup 20 --runs 200`; p50 and p90 from the exported JSON.

| Binary | Mean | p50 / p90 |
| --- | --- | --- |
| empty Go `main` | 2.7 ± 0.2 ms | 2.65 / 2.91 ms |
| YAML decoder probe | 3.4 ± 0.5 ms | 3.36 / 3.92 ms |
| CUE linked, no load | 8.6 ± 2.2 ms | 8.16 / 9.70 ms |
| CUE load of a profile | 9.0 ± 0.9 ms | 8.79 / 9.54 ms |
| empty `main` plus a 15 MB blob | 3.9 ± 2.0 ms | — |

The cost is package init, not binary size. `GODEBUG=inittrace=1` lists
`cockroachdb/apd/v3` as the largest single package. The Bun base in
`effect4-cli.md` is 18.8 ms p50.

## Why not the others

Sources were read on 2026-10-05. Commit-pinned URLs are used where the
claim is about a file at the head of a branch.

### Pkl

[apple/pkl-go `pkl/evaluator_manager_exec.go` at v0.14.0](https://github.com/apple/pkl-go/blob/v0.14.0/pkl/evaluator_manager_exec.go),
symbols `getCommandAndArgStrings` and `init`. The Go binding starts the
`pkl` CLI as a child process:

```go
	return "pkl", []string{}
}

func (e *execEvaluator) init() error {
	e.cmd = e.getStartCommand()
	...
	err = e.cmd.Start()
```

The nixpkgs `pkl` 0.31.1 is the JVM build. Commands and output:

```text
$ nix path-info -rSh nixpkgs#pkl
/nix/store/va7s008nkmzm8bd6p5qps8f93wbjjyyj-temurin-bin-21.0.12  334.9 MiB
/nix/store/waizglgrr08fy6mp2832k4bpmjbi2p2i-pkl-0.31.1           355.3 MiB

$ nix shell nixpkgs#pkl nixpkgs#hyperfine --command \
    hyperfine -N --warmup 3 --runs 10 'pkl eval -f json my.pkl'
  Time (mean ± σ):     645.6 ms ±  41.2 ms    [User: 1091.9 ms, System: 75.2 ms]
  Range (min … max):   597.1 ms … 706.3 ms    10 runs
```

`my.pkl` is a profile-sized file that amends a typed `TessSchema.pkl`.

### KCL

[kcl-lang/kcl-go `go.mod` at 71884e1](https://github.com/kcl-lang/kcl-go/blob/71884e186f22810ceb68bab222c11d32e393068c/go.mod):

```text
replace kcl-lang.io/lib => ./kcl-lang-lib
```

[kcl-lang/lib `go/native/loader.go` at 2761803](https://github.com/kcl-lang/lib/blob/2761803f2737d45304801b9f4ea62f9a8db7d572/go/native/loader.go)
installs the native library, then opens it:

```go
	err = install.InstallKcl(libPath)
	...
	libm, err := openLibrary(libFullPath)
```

[`go/native/open_lib_unix.go`](https://github.com/kcl-lang/lib/blob/2761803f2737d45304801b9f4ea62f9a8db7d572/go/native/open_lib_unix.go),
symbol `openLibrary`:

```go
	return purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
```

### Nickel

[nickel-lang/go-nickel `README.md` at 827dc61](https://github.com/nickel-lang/go-nickel/blob/827dc613bd42a2ec9bca0ea822e4823b63e9142e/README.md):

```text
(You need to use `main` because we haven't had any releases yet.)
```

[`nickel.go`](https://github.com/nickel-lang/go-nickel/blob/827dc613bd42a2ec9bca0ea822e4823b63e9142e/nickel.go),
the cgo preamble:

```go
#cgo darwin,arm64 LDFLAGS: ${SRCDIR}/lib/darwin_arm64/libnickel_lang.a -lm
...
import "C"
```

### Dhall

[philandstuff/dhall-golang latest release](https://github.com/philandstuff/dhall-golang/releases/tag/v6.0.2),
from `GET https://api.github.com/repos/philandstuff/dhall-golang/releases/latest`:

```text
"tag_name": "v6.0.2",
"published_at": "2021-10-09T11:39:48Z",
```

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
| `github.com/nightlyone/lockfile` | 414 | v1.0.0 | 2020-03-08 | 2021-11-04 `bf01bef` |
| `github.com/bep/debounce` | 277 | v1.2.1 | 2022-05-15 | 2026-08-21 `0ed0c00` |
| `github.com/kardianos/service` | 1,591 | v1.3.0 | 2026-07-06 | 2026-08-29 `9907089` |
| `howett.net/plist` | 543 | v1.0.1 | 2023-10-24 | 2026-08-19 `760f9a5` |
| `github.com/adrg/xdg` | 1,197 | v0.5.3 | 2024-10-31 | 2026-09-15 `424b3a2` |
| `golang.org/x/sync/errgroup` | 30,110 | v0.23.0 | 2026-08-31 | 2026-09-23 `36f2d70` |
| `github.com/samber/lo` | 12,533 | v1.53.0 | 2026-03-02 | 2026-10-01 `5c6ddcb` |
| `github.com/samber/mo` | 434 | v1.17.0 | 2026-06-02 | 2026-10-01 `502998f` |
| `github.com/deckarep/golang-set/v2` | 1,842 | v2.9.0 | 2026-04-21 | 2026-10-02 `20c6d8d` |
| `cuelang.org/go/cue` | 692 | v0.17.1 | 2026-07-16 | 2026-10-05 `0548724` |
| `github.com/stretchr/testify/require` | 20,797 | v1.12.1 | 2026-08-17 | 2026-09-24 `87a7b9d` |
| `github.com/google/go-cmp/cmp` | 5,704 | v0.7.0 | 2025-01-14 | 2026-06-18 `b133f1f` |
| `gotest.tools/v3/assert` | 1,570 | v3.5.2 | 2024-09-05 | 2026-09-14 `749748e` |
| `gotest.tools/v3/golden` | 6 | v3.5.2 | 2024-09-05 | 2026-09-14 `749748e` |
| `github.com/sebdah/goldie/v2` | 19 | v2.8.0 | 2025-10-11 | 2025-11-22 `5baf619` |

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
| cobra | kubernetes, cli/cli, hugo, helm, golangci-lint, grafana, docker/buildx, cue (v1.10.2) |
| urfave/cli | gitea (v3.13.0); go-ethereum and grafana (v2) |
| kong | hermit (v1.16.1), block/ftl (v1.11.0) |
| gofrs/flock | helm, golangci-lint, docker/buildx, moby, traefik (v0.13.1); prometheus (v0.13.0); go-ethereum (v0.12.1) |
| samber/lo | terraform, lazygit, traefik (v1.53.0) |
| golang-set | go-ethereum (v2.6.0) |
| gotest.tools/v3 | docker/cli, moby, cli/cli (v3.5.2) |
| x/sync | kubernetes, cli/cli, hugo, helm, golangci-lint, cue (v0.23.0) |
| go-cmp | kubernetes, cli/cli, helm, prometheus, cue (v0.7.0) |

cobra is already in tess's module graph:
[cue-lang/cue `go.mod` at v0.17.1](https://github.com/cue-lang/cue/blob/v0.17.1/go.mod)
requires it.

```text
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
```

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

cobra adds about 0.9 ms at p50. That is small next to the CUE load
(§ Process start).

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
