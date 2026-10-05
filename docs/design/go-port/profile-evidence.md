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

| Language | Primary-source finding |
| --- | --- |
| Pkl | pkl-go `getCommandAndArgStrings` returns `"pkl", []string{}` and starts it as a subprocess. The nixpkgs `pkl` is the JVM build: 355 MiB closure, about 861 ms per `pkl eval`. |
| KCL | kcl-go `go.mod`: `replace kcl-lang.io/lib => ./kcl-lang-lib`; the lib loads a native `libkcl` at runtime. |
| Nickel | go-nickel README: "we haven't had any releases yet"; `nickel.go` has `#cgo darwin,arm64 LDFLAGS: ${SRCDIR}/lib/darwin_arm64/libnickel_lang.a`. |
| Dhall | dhall-golang latest release v6.0.2, published 2021-10-09. |
