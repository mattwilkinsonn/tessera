# Design: prebuilt `tess` release binaries

*Design record for RIG-8649. Matt's direction (2026-11-08) is fixed: build the darwin binary on every PR, reuse that build on main when the trees match, publish it as a GitHub release asset with a nix flake output and a Homebrew cask, and point Matt's nix config at the prebuilt output so `nix-switch` stops compiling tess.*

## Open Questions

1. **How is the cask signed?** Options: (a) ad-hoc signature, which the Go and nix toolchains already apply, plus a cask `postflight` that removes the quarantine attribute; (b) Developer ID signature and Apple notarization. Option (a) costs nothing. It bypasses Gatekeeper for cask users, and Homebrew discourages it. Option (b) needs a paid Apple Developer Program membership, which is Matt's spend decision. It also needs a Developer ID certificate and a notary API key as Actions secrets, which Nemo sets up. Nix users are not affected either way, because `fetchurl` sets no quarantine attribute. **Recommendation:** (a) now. The cask serves other users, and Matt installs through nix. Move to (b) if the cask gets real users. § *Gatekeeper* has the evidence.
2. **Which main merges publish a binary?** Options: (a) every main merge publishes a `build-<sha12>` prerelease, and a `VERSION` bump also publishes the stable `v<VERSION>` release as today; (b) only `VERSION` bumps publish, and Matt's config follows release tags instead of main. Option (a) keeps today's habit: Matt's config follows main commits, and `VERSION` has been 0.2.0 since 2026-10-04 while fixes kept landing. It adds one prerelease per merge to the releases list. Option (b) keeps the list clean, but every fix Matt wants deployed needs a `VERSION` bump PR. **Recommendation:** (a). The cask tracks stable releases only under both options.

## Problem / Intent

After a tessera bump, `nix-switch` compiles `tess` from source on Matt's Mac. CI already builds the same binary on every PR and throws the result away. Publish the CI build once and serve it to nix and Homebrew, so a tessera bump downloads a binary instead of compiling it.

## Direction

- **Product fit.** RIG-8649 asks for a faster `nix-switch` after a tessera bump. The release asset and cask also give non-nix users an install path. The README has no install section today.
- **Scope across repos.** tessera (CI, release workflow, flake output, a small Go release tool, README). `mattwilkinsonn/homebrew-tap` (one cask, written by the release bot). Matt's nix config in orion: a one-line package switch, named here as the downstream consumer only.
- **Prior art.**
  - [oxalica/rust-overlay](https://github.com/oxalica/rust-overlay) serves prebuilt toolchains from a flake. A bot commits the hashes into the repo: "Hashes of toolchain components are pre-fetched in tree, so the evaluation is *pure* … These hashes are auto-updated daily using GitHub Actions." Borrow: a CI-committed pin file inside the flake repo.
  - [mitchellh/zig-overlay](https://github.com/mitchellh/zig-overlay): "The flake mirrors the binaries built officially by Zig and does not build them from source." It reads URLs and hashes from a committed `sources.json`. Borrow: the JSON pin format, read with `builtins.fromJSON`.
  - The `mattwilkinsonn/jj-hooks` release workflow bumps `Formula/jj-hooks.rb` in `mattwilkinsonn/homebrew-tap` with a `zireael-release` GitHub App token: "The App is in the bypass actors list for the main-branch ruleset". Borrow: the same App and token step for the cask.
- **Lifetime.** This runs on every merge for as long as tessera ships darwin binaries. The design keeps the moving parts few: one CI step, one workflow, one pin file and one Go tool.
- **Simpler option.** A nix binary cache reaches the same `nix-switch` speedup with less machinery. § *Alternatives considered* compares it fairly. Matt's direction stands, because it also gives non-nix users a release asset and a cask.

## Approach

### Build and reuse (item 2)

The `nix` job in `.github/workflows/ci.yml` already runs `nix build .#default` on `macos-latest`. It gains three steps after `Verify tess version`:

1. `codesign --verify --strict result/bin/tess` confirms the binary carries a valid signature. Apple silicon needs one: macOS will not "permit native arm64 code to execute unless a valid signature is attached" ([Apple, Rosetta 2 on a Mac with Apple silicon](https://support.apple.com/guide/security/rosetta-2-on-a-mac-with-apple-silicon-secebb113be1/1/web/1), quoted in [Homebrew/brew#20755](https://github.com/Homebrew/brew/issues/20755)). The Go linker ad-hoc signs darwin binaries: "Package codesign provides basic functionalities for ad-hoc code signing of Mach-O files" ([cmd/internal/codesign](https://pkg.go.dev/cmd/internal/codesign)).
2. Pack `bin/tess` and `share/tessera/schema.cue` from the realised `result` path into `tess-darwin-arm64.tar.gz`. A tarball keeps the exec bit, which a raw artifact upload loses: "Zipping files will remove file permissions during artifact upload" ([actions/download-artifact README](https://github.com/actions/download-artifact#maintaining-file-permissions)). The schema must ship because consumers validate profiles against `${package}/share/tessera/schema.cue`.
3. Upload it as the artifact `tess-darwin-arm64-<tree>`, with `retention-days: 30`. `<tree>` is `git rev-parse HEAD^{tree}` of the checked-out commit.

**Why the tree is the key.** `src = ./.` and `flake.lock` are both in the tree, and nothing else feeds the build. Two commits with one tree therefore build the same derivation, so a hit is always correct and a miss only costs a rebuild. A squash merge of a PR that is up to date with main has the PR head's tree. A `pull_request` run checks out GitHub's test merge commit, so its tree also equals the squash result whenever main has not moved since the run [INFERENCE: both merges come from the same inputs].

**Main reuse.** On a push to main, the `nix` job first lists artifacts by exact name: `GET /repos/{owner}/{repo}/actions/artifacts?name=…` ("When specified, only artifacts with this name will be returned", [REST: artifacts](https://docs.github.com/en/rest/actions/artifacts#list-artifacts-for-a-repository)). It keeps unexpired artifacts whose `workflow_run.head_repository_id` equals `workflow_run.repository_id`, and takes the newest. Only same-repo branches count, because a fork PR runs its own workflow file and could upload bytes under a predicted tree name. On a hit, the job skips Install Nix, flake check and build. On a miss, it runs the job as today and uploads the artifact under the main tree, which is the fallback rebuild.

**Retention.** "By default … artifacts and log files generated by workflows are retained for 90 days", and public repositories can set "anywhere between 1 day or 90 days" ([Managing GitHub Actions settings](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository#configuring-the-retention-period-for-github-actions-artifacts-and-logs-in-your-repository)). 30 days covers a normal PR. A PR that merges later falls back to the rebuild.

### Release (items 3 and 5)

`.github/workflows/release.yml` changes its trigger from `push` to `workflow_run` of `ci`, `types: [completed]`, `branches: [main]`. The job runs only when the run concluded `success`, its event is `push`, and its head repository is this repository. Releases now wait for a green main, and the artifact always exists, from either the PR run or the main run. The job:

1. Finds and downloads `tess-darwin-arm64-<tree>` with `actions/download-artifact` using `run-id` and `github-token` ("you can specify a `github-token` along with other repository and run identifiers", [README](https://github.com/actions/download-artifact#download-artifacts-from-other-workflow-runs-or-repositories)).
2. Picks the tag. If `v<VERSION>` has no release, it creates the stable release at `head_sha` with `--generate-notes`, as today. Otherwise it creates `build-<sha12>` with `--prerelease --latest=false` (Open Question 2).
3. Uploads `tess-darwin-arm64.tar.gz`, downloads it again and checks the sha256.
4. Writes `nix/prebuilt.json` with `tools/releasepin` and pushes `chore(release): pin tess <tag> [skip ci]` to main with `GITHUB_TOKEN`. tessera has no ruleset on main. `[skip ci]` stops both workflows: "Workflows that would otherwise be triggered using `on: push` or `on: pull_request` won't be triggered" ([Skipping workflow runs](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/skip-workflow-runs)). With no `ci` run, `workflow_run` cannot loop.
5. On a stable release only, it renders `Casks/tessera.rb` into the tap and pushes it with the App token (§ *Tap*).

A release is never deleted once a pin names it, because old pins in consumers' lock files would stop fetching.

**Concurrency.** The existing group stays, with `cancel-in-progress: false`. A newer pending run replaces an older pending one, so rapid merges can skip a build release. That is harmless, because the next pin covers the newer commit. A skipped `VERSION` bump is still released by the next run that sees no `v<VERSION>` release, as today. `releasepin` refuses to write a pin whose `rev` is not a descendant of the current pin's `rev`, so an out-of-order run cannot move the pin backwards.

**Tool: hand-written, not GoReleaser.** GoReleaser OSS builds its own binaries, and importing a binary built elsewhere is Pro-only: "This feature is exclusively available with GoReleaser Pro" ([Import pre-built binaries](https://goreleaser.com/customization/builds/builders/prebuilt)). Using it would rebuild on main and break the reuse rule. Its `nix` publisher writes a standalone derivation into a NUR-style repository ("generate and publish a nix derivation to an existing Nix User Repository", [nix publisher](https://goreleaser.com/customization/publish/nix/)), with default path `pkgs/<name>/default.nix`. That file is not wired to this flake's home-manager module, so we would still need glue. Its `homebrew_casks` publisher ([Homebrew Casks](https://goreleaser.com/customization/publish/homebrew_casks/), "Since v2.10") also needs GoReleaser-built archives. The hand-written path is about 40 workflow lines and one Go tool. The Windows record's W7 (`windows-backend/design.md`) still adds GoReleaser for the Windows ZIPs and Scoop with `release.mode: append`. It slots into the stable-release branch of step 2 and builds only `windows/*`.

### Flake output: where the URL and hash live (item 1)

A flake at commit X cannot contain the hash of the binary built from X. Three options:

| Option | How | Cost |
| --- | --- | --- |
| **A. Bot-committed pin (chosen)** | After release, CI commits `nix/prebuilt.json` (`version`, `rev`, `url`, `hash`) to main. The flake reads it. | One `[skip ci]` commit per release. The pin at main's tip names the binary of its parent commit, whose source is identical. During a merge race, the pin trails main by one merge until the next release. |
| B. Separate flake or repo | A `tessera-bin` repo holds the pin and a flake that exposes the package. | No bot commits on tessera main. Consumers bump two inputs, and the module and binary versions can drift apart. |
| C. Consumer pins url+hash | Matt's config calls `fetchurl` itself with a URL and hash it edits on each bump. | No tessera machinery. Every bump becomes a manual two-string edit, and tessera ships no flake output, contrary to the direction. |

The prior art (rust-overlay, zig-overlay) uses A. The flake adds `packages.aarch64-darwin.prebuilt`, built only when `nix/prebuilt.json` exists, because it does not exist until the first release run:

```nix
prebuilt = pkgs.stdenvNoCC.mkDerivation {
  pname = "tess";
  inherit (pin) version;
  src = pkgs.fetchurl { inherit (pin) url hash; };
  sourceRoot = ".";
  dontConfigure = true;
  dontBuild = true;
  dontFixup = true; # keep the CI signature intact
  installPhase = "mkdir -p $out && cp -R bin share $out/";
  meta = {
    mainProgram = "tess";
    platforms = [ "aarch64-darwin" ];
    sourceProvenance = [ pkgs.lib.sourceTypes.binaryNativeCode ];
  };
};
```

`packages.default` stays the source build, and the module's `package` default does not change. The source build's `src` excludes `nix/prebuilt.json` through `lib.fileset`, so a pin commit does not change the source derivation.

### Tap and token (items 4 and 6)

**Tap: `mattwilkinsonn/homebrew-tap`.** tessera is a `mattwilkinsonn` repository, and that tap already serves `jj-hooks` and `jj-gt`. It is written by `zireael-release[bot]`. `RigelBuild/homebrew-tap` holds only `Formula/compass-cli.rb`, is written by Rigel's CI app and has CODEOWNERS. It is the Rigel product tap. The cask goes in `Casks/tessera.rb`, which is a new directory in this tap. The token is `tessera` (the project name), and the cask installs `binary "bin/tess"` with `depends_on arch: :arm64`.

**Token.** A cross-repo push cannot use `GITHUB_TOKEN`: "you cannot use the default action token. You must use a separate token with content write privileges for the tap repository" ([Homebrew Casks](https://goreleaser.com/customization/publish/homebrew_casks/)). Reuse the jj-hooks pattern: `actions/create-github-app-token` with `app-id: ${{ vars.RELEASE_BOT_APP_ID }}` and `private-key: ${{ secrets.RELEASE_BOT_PRIVATE_KEY }}`, `owner: mattwilkinsonn`, `repositories: homebrew-tap`. The App needs contents:write, and it is already a bypass actor on the tap's main ruleset. tessera has neither the variable nor the secret today (`gh variable list` is empty; `gh secret list` shows only `SCOOP_BUCKET_TOKEN`). **Nemo** installs the App on tessera and adds both values. The release workflow does not run until that is done.

### Gatekeeper and quarantine (item 4)

Homebrew sets `com.apple.quarantine` on cask downloads, and a quarantined binary must pass Gatekeeper, which wants Developer ID plus notarization. GoReleaser's cask docs say so plainly: "Casks are supposed to be signed, even if they are coming from a tap", and without that users see "*App Name is damaged and cannot be opened*". Homebrew removed the user-side escape. [Homebrew/brew#20755](https://github.com/Homebrew/brew/issues/20755) deprecates `--no-quarantine` ("This is used to run unsigned/unnotarized applications") and says "we are ending support for all casks that fail Gatekeeper checks on September 1st, 2026". That date applies to the official cask repository. A third-party tap can still ship a `postflight` that runs `xattr -d com.apple.quarantine` on `#{staged_path}/bin/tess`, which is GoReleaser's documented fallback. The same docs warn that it "circumvents macOS security protections" and that "Apple may disable this bypass method in future macOS versions". Maintainers on #20755 also floated an audit warning for third-party taps that do it.

- **Ad-hoc (default, Open Question 1).** Zero cost and no credentials. The cask carries the `postflight` and a `caveats` line that says why.
- **Developer ID + notarization.** Needs the Apple Developer Program, which "Apple charges a yearly fee for" ([Homebrew Casks § Signing and Notarizing](https://goreleaser.com/customization/publish/homebrew_casks/#signing-and-notarizing)). It also needs a Developer ID Application certificate and a notary API key stored as tessera secrets. That is credential setup, so it goes to **Nemo**; the spend is Matt's. The sign and notarize step runs in the PR build, so main still reuses the exact bytes.

Nix is unaffected. `fetchurl` writes into the store without a quarantine attribute, and the CI ad-hoc signature satisfies Apple silicon.

### Matt's config and the speedup (item 7)

The consumer change is one line in the home-manager config, keeping the `follows` on the tessera input:

```nix
programs.tessera.package = inputs.tessera.packages.${pkgs.stdenv.hostPlatform.system}.prebuilt;
```

The prebuilt derivation uses the consumer's nixpkgs only for `stdenvNoCC` and `fetchurl`. The `fetchurl` output is fixed-output and keyed by its hash, so it is fetched once per tessera bump and stays cached across nixpkgs bumps. The `follows` that makes tessera CI's output path differ from Matt's no longer matters.

**Where the build time goes**, measured from the `nix` job of ci run [42508101101](https://github.com/mattwilkinsonn/tessera/actions/runs/42508101101) on main `040760ea` (`macos-26-arm64` runner, cold store). `Build tess` took 78 s:

| Phase | Time | Rebuilt on Matt's Mac after a tessera bump? |
| --- | --- | --- |
| Eval and substitution plan | 4 s | Eval, yes |
| Fetch 67 paths (275.4 MiB) + Go tarball | 11 s | No, already in the store |
| `go-1.27.3` (unpack toolchain) | 17 s | Only when nixpkgs or go-overlay moves |
| `tess-0.2.0-go-modules` (fixed-output) | 6 s | Only when `vendorHash` or `VERSION` changes, because its name carries the version |
| `tess-0.2.0` compile + install check | 38 s | Yes, every bump |

So a source-only tessera bump costs about 38 s of runner time for the compile and install check. A bump that comes with a nixpkgs-unstable update costs about 55 s or more. The prebuilt path replaces both with one download of a few MB plus a trivial unpack [INFERENCE: the tarball size is unmeasured].

**Unmeasured:** compile time on Matt's Mac, which likely has more cores than the 3-vCPU runner [INFERENCE]; total `nix-switch` wall time and tess's share of it; and eval time for fetching the tessera input, which this change does not remove. Task 4's gate measures the first two.

### Alternatives considered

- **Nix binary cache instead (item 8).** Push the CI `nix build` output to Cachix with `cachix/cachix-action` (`authToken: "${{ secrets.CACHIX_AUTH_TOKEN }}"`), add the cache as a substituter, and drop `inputs.nixpkgs.follows` from Matt's tessera input, so his derivation hash equals CI's. Cachix is free at this size: "Users have a free 5 GB limit for open source projects" ([pricing](https://www.cachix.org/pricing)). The existing tailnet Attic cache cannot serve GitHub-hosted runners without a tailnet join. That is about ten lines across two repos, with no pin file, no bot commit, no release machinery and no hash bookkeeping, and `tess` stays a normal source derivation. Its costs: Matt's tessera input stops following his nixpkgs, so it evaluates against tessera's own nixpkgs pin, though only the tiny `tess` closure is substituted; it adds a Cachix account and token through Nemo; a substituter outage silently falls back to a local compile; and it serves nix only, with no release asset or cask. It reaches the same `nix-switch` speedup more cheaply. Matt's direction chose the release path because it also serves Homebrew and plain downloads.
- **Predict the PR's own hash.** A bot commits the PR's binary hash into the PR branch, because the pin file is not a build input. Rejected: it pushes commits onto agents' jj-vine branches, fails on fork PRs, and depends on byte-reproducible tarballs.
- **One rolling `builds` release with tree-named assets.** Rejected: "Up to 1000 release assets may be associated with a single release" ([About releases](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases)), so per-merge assets would hit the cap, and pruning would break old pins.

## Global Constraints

- **Platform:** `aarch64-darwin` only. The runner is `macos-26-arm64`, and Matt's Macs are Apple silicon. No `x86_64-darwin` asset.
- **Asset contract:** asset name `tess-darwin-arm64.tar.gz`, containing `bin/tess` and `share/tessera/schema.cue` at the archive root. URL `https://github.com/mattwilkinsonn/tessera/releases/download/<tag>/tess-darwin-arm64.tar.gz`. Tags `v<VERSION>` (stable) and `build-<sha12>` (prerelease, not latest).
- **Pin contract:** `nix/prebuilt.json` = `{"version": "0.2.0", "rev": "<40-hex sha>", "url": "<asset URL>", "hash": "sha256-<base64>"}`. Only `tools/releasepin` writes it.
- **Artifact key:** `tess-darwin-arm64-<git rev-parse HEAD^{tree}>`, retention 30 days, trusted only from same-repo runs.
- **Bytes shipped = bytes tested:** release, pin and cask all use the CI artifact. No release job compiles.
- **No release deletion** once a pin names it.
- **Toolchain:** Go 1.27.3 for `tools/releasepin`, matching `go.mod` `toolchain go1.27.3`. Existing action majors (`actions/checkout@v4`, `actions/setup-go@v6`).
- **Credentials go to Nemo:** the `zireael-release` App install on tessera, `RELEASE_BOT_APP_ID` and `RELEASE_BOT_PRIVATE_KEY`, and any Apple signing material.
- **Public repo:** name orion only as the downstream consumer. No private paths or hosts in tessera files.
- **Cost:** GitHub-hosted runners and artifact storage on a public repository add no billed meter.
- **Comments:** 1-2 lines saying why. No issue IDs in source.

## Plan

### Task 1: PR build artifact and main reuse (`ci.yml`)

Interfaces:

- `nix` job gains `permissions: { contents: read, actions: read }`.
- Step `key`: `echo "tree=$(git rev-parse 'HEAD^{tree}')" >> "$GITHUB_OUTPUT"`.
- Step `reuse` (push to main only): `gh api "repos/$GITHUB_REPOSITORY/actions/artifacts?name=tess-darwin-arm64-$TREE"`, filtered by jq to `expired == false and .workflow_run.head_repository_id == .workflow_run.repository_id`, newest first. Outputs `hit=true|false`.
- Install Nix, Flake check, Build, Verify, the new `codesign --verify --strict result/bin/tess`, Pack and Upload steps all run `if: steps.reuse.outputs.hit != 'true'`.
- Pack: `tar -czf tess-darwin-arm64.tar.gz -C "$(readlink -f result)" bin share`.
- Upload: `actions/upload-artifact@v4`, `name: tess-darwin-arm64-${{ steps.key.outputs.tree }}`, `retention-days: 30`, `if-no-files-found: error`.

Gate: on a PR, the run shows the artifact and `codesign` passes. After an up-to-date merge, the main run logs a hit and skips `nix build`. A main push with no matching artifact rebuilds and uploads.

### Task 2: `tools/releasepin` and the `prebuilt` flake output

Interfaces:

- `tools/releasepin/main.go`, following `tools/benchstartup`. Subcommands:
  - `releasepin pin -tarball PATH -tag TAG -rev SHA -version V -repo OWNER/NAME -out nix/prebuilt.json`. It writes the pin. It exits 3 without writing when the existing pin's `rev` is not an ancestor of `-rev` (`git merge-base --is-ancestor`).
  - `releasepin cask -tarball PATH -version V -repo OWNER/NAME -out PATH`. It renders `Casks/tessera.rb` with a hex `sha256`, `url` on the `v#{version}` asset, `binary "bin/tess"`, `depends_on arch: :arm64`, the quarantine `postflight` and `caveats`.
- `type Pin struct { Version, Rev, URL, Hash string }` with JSON tags `version`, `rev`, `url`, `hash`. `func sriSHA256(b []byte) string` returns `"sha256-" + base64(sha256(b))`.
- `flake.nix`: `pin = builtins.fromJSON (builtins.readFile ./nix/prebuilt.json)`. `packages.prebuilt` as in § *Flake output*, added with `lib.optionalAttrs (system == "aarch64-darwin" && builtins.pathExists ./nix/prebuilt.json)`. The source build's `src` becomes `lib.fileset.toSource { root = ./.; fileset = lib.fileset.difference ./. (lib.fileset.maybeMissing ./nix/prebuilt.json); }`.

Gate: table tests cover pin JSON and SRI for a fixed input, the ancestor refusal (exit 3), and a golden cask file. `go vet`, `golangci-lint`, `nilaway` and `go test -race ./tools/...` pass. `nix flake check` passes with and without `nix/prebuilt.json`. With a hand-made pin pointing at a test asset, `nix build .#prebuilt` yields a `tess` that prints `tess v0.2.0`.

### Task 3: release workflow

Interfaces:

- `release.yml`: `on: workflow_run: { workflows: [ci], types: [completed], branches: [main] }`. Job `if:` `conclusion == 'success' && event == 'push' && head_repository.full_name == github.repository`. `permissions: { contents: write, actions: read }`. The existing concurrency group stays.
- Steps: checkout at `head_sha` with `fetch-depth: 0`; setup-go; artifact lookup as in Task 1, failing if absent; `actions/download-artifact` with `run-id` and `github-token`; tag choice; `gh release create` (stable: `--target "$SHA" --generate-notes`; build: `--target "$SHA" --prerelease --latest=false --notes "Build of $SHA"`); `gh release upload`; re-download and compare `sha256sum`; `git checkout -B main origin/main`; `releasepin pin`; commit `chore(release): pin tess <tag> [skip ci]` and push with up to 3 fetch-rebase retries. Exit 3 from `releasepin` skips the commit.
- Stable releases only: `actions/create-github-app-token` (jj-hooks inputs, `repositories: homebrew-tap`); checkout `mattwilkinsonn/homebrew-tap` at `main` with that token; `releasepin cask`; commit `chore(tap): bump tessera to v<VERSION>`; push.
- README gains `## Install`: nix (`packages.<system>.prebuilt` or `programs.tessera.package`), Homebrew (`brew install --cask mattwilkinsonn/tap/tessera`) and the quarantine caveat.

Gate: `actionlint` passes. The first main merge after Nemo's setup produces a `build-<sha12>` prerelease with the asset, a pin commit that skips CI, and no second release run. The next `VERSION` bump produces `v<VERSION>` and the tap commit. `brew install --cask mattwilkinsonn/tap/tessera && tess --version` works on a Mac.

### Task 4: switch Matt's config (orion)

Interfaces: one line, `programs.tessera.package = inputs.tessera.packages.${pkgs.stdenv.hostPlatform.system}.prebuilt;`, after `nix flake update tessera` lands on a commit that has a pin. The existing profile check keeps reading `${config.programs.tessera.package}/share/tessera/schema.cue`.

Gate: a `--dry-run` build of the MacBook's darwin configuration lists no `go-1.27.3`, `tess-*-go-modules` or source `tess-*` derivation to build. Before/after timings of that build on Matt's Mac after a tessera bump are recorded in the PR. The driver runs these as plain `nix build`, without switching.

## Tasks

- [ ] Task 1 — PR build artifact keyed by tree, main reuse with fallback rebuild (`ci.yml`)
- [ ] Task 2 — `tools/releasepin` and the `prebuilt` flake output
- [ ] Task 3 — `workflow_run` release: asset, pin commit, cask, README (needs Nemo's App install and secrets on tessera)
- [ ] Task 4 — orion: point `programs.tessera.package` at `prebuilt`, with measured timings
