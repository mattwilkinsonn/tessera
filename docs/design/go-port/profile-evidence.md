# Go port — profile language evidence

*Supporting file for [`design.md`](design.md) § Profile format. All probes
ran on macOS arm64 with cue v0.17.1, `cuelang.org/go` v0.17.1, go1.26.7,
pkl 0.31.1 and hyperfine 1.20.0, from nixpkgs.*

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
