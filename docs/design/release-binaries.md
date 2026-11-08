# Design: prebuilt `tess` release binaries

*Design record for RIG-8649. Matt's direction (2026-11-08) is fixed: build the darwin binary on every PR, reuse that build on main when the trees match, publish it as a GitHub release asset with a nix flake output and a Homebrew cask, and point Matt's nix config at the prebuilt output so `nix-switch` stops compiling tess.*

## Resolved decisions

1. **Cask signing (Matt, RIG-8652, 2026-11-08).** Ad-hoc signature now, with a cask `postflight` that removes the quarantine attribute, as § *Gatekeeper* describes. Developer ID signing and notarization follow once the DUNS number and an Apple Developer Program membership are in place. Nix users are not affected either way, because `fetchurl` sets no quarantine attribute.
2. **Which main merges publish (Matt, RIG-8652, 2026-11-08, option A).** Every main merge publishes a `build-<sha12>` prerelease. A `VERSION` bump also publishes the stable `v<VERSION>` release. The cask follows stable releases only. Matt's config follows main, as today.

## Problem / Intent

After a tessera bump, `nix-switch` compiles `tess` from source on Matt's Mac. CI already builds the same binary on every PR and throws the result away. Publish the CI build once and serve it to nix and Homebrew, so a tessera bump downloads a binary instead of compiling it.

## Direction

- **Product fit.** RIG-8649 asks for a faster `nix-switch` after a tessera bump. The release asset and cask also give non-nix users an install path. The README has no install section today.
- **Scope across repos.** tessera (CI, release workflow, flake output, a small Go release tool, README). `mattwilkinsonn/homebrew-tap` (one cask, written by the release bot). Matt's nix config in orion: a one-line package switch, named here as the downstream consumer only.
- **Prior art.**
  - [oxalica/rust-overlay](https://github.com/oxalica/rust-overlay) serves prebuilt toolchains from a flake. A bot commits the hashes into the repo: "Hashes of toolchain components are pre-fetched in tree, so the evaluation is *pure* … These hashes are auto-updated daily using GitHub Actions." Borrow: a CI-committed pin file inside the flake repo.
  - [mitchellh/zig-overlay](https://github.com/mitchellh/zig-overlay): "The flake mirrors the binaries built officially by Zig and does not build them from source." It reads URLs and hashes from a committed `sources.json`. Borrow: the JSON pin format, read with `builtins.fromJSON`.
  - The `mattwilkinsonn/jj-hooks` release workflow bumps `Formula/jj-hooks.rb` in `mattwilkinsonn/homebrew-tap` with a `zireael-release` GitHub App token: "The App is in the bypass actors list for the main-branch ruleset". Borrow: the same App and token step for the cask.
- **Lifetime.** This runs on every merge for as long as tessera ships darwin binaries. The design keeps the moving parts few: CI steps in one job, one called release workflow, one pin file and one Go tool. Its stages (release, asset, pin, tap) can each fail on their own, so each one converges on a target state and a rerun is always safe (§ *Recovery*).
- **Simpler option.** A nix binary cache reaches the same `nix-switch` speedup with less machinery. § *Alternatives considered* compares it fairly. Matt's direction stands, because it also gives non-nix users a release asset and a cask.

## Approach

### Build and reuse (item 2)

The `nix` job in `.github/workflows/ci.yml` already runs `nix build .#default` on `macos-latest`. It gains three steps after `Verify tess version`:

1. `codesign --verify --strict result/bin/tess` confirms the binary carries a valid signature. Apple silicon needs one: macOS will not "permit native arm64 code to execute unless a valid signature is attached" ([Apple, Rosetta 2 on a Mac with Apple silicon](https://support.apple.com/guide/security/rosetta-2-on-a-mac-with-apple-silicon-secebb113be1/1/web/1), quoted in [Homebrew/brew#20755](https://github.com/Homebrew/brew/issues/20755)). The Go linker ad-hoc signs darwin binaries: "Package codesign provides basic functionalities for ad-hoc code signing of Mach-O files" ([cmd/internal/codesign](https://pkg.go.dev/cmd/internal/codesign)).
2. Pack `bin/tess` and `share/tessera/schema.cue` from the realised `result` path into `tess-darwin-arm64.tar.gz` under `$RUNNER_TEMP/out`. A tarball keeps the exec bit, which a raw artifact upload loses: "Zipping files will remove file permissions during artifact upload" ([actions/download-artifact README](https://github.com/actions/download-artifact#maintaining-file-permissions)). The schema must ship because consumers validate profiles against `${package}/share/tessera/schema.cue`. Next to it, `releasepin provenance` writes `provenance.json` with the checked-out commit (`github.sha`) and the tarball's sha256. On `pull_request` that commit is GitHub's test merge commit; on `push` it is the pushed commit.
3. Upload `$RUNNER_TEMP/out` as the artifact `tess-darwin-arm64-<tree>`. `<tree>` is `git rev-parse HEAD^{tree}` of the checked-out commit. Retention is 30 days on PR runs and 90 days on main runs.

**Why the tree is the key.** `src = ./.` and `flake.lock` are both in the tree, and nothing else feeds the build. Two commits with one tree therefore build the same derivation, so a hit is always correct and a miss only costs a rebuild. A squash merge of a PR that is up to date with main has the PR head's tree. A `pull_request` run checks out GitHub's test merge commit, so its tree also equals the squash result whenever main has not moved since the run [INFERENCE: both merges come from the same inputs]. The key compares trees, never commit SHAs: the PR run's `head_sha` is the PR head, not the test merge, and neither equals the squash SHA. The tree also contains `nix/prebuilt.json`. When a pin commit lands on main after a PR's last run, the trees differ and main rebuilds. That miss is safe, and the design does not promise a hit in that case.

**Main reuse (handoff and provenance).** On a push to main, the `nix` job runs `releasepin select` before Install Nix. It never trusts the artifact name alone:

1. It lists unexpired artifacts named `tess-darwin-arm64-<tree>`, newest first: `GET /repos/{owner}/{repo}/actions/artifacts?name=…` ("When specified, only artifacts with this name will be returned", [REST: artifacts](https://docs.github.com/en/rest/actions/artifacts#list-artifacts-for-a-repository)).
2. For each candidate it fetches the producer run, `GET /actions/runs/{workflow_run.id}`, and keeps it only when all of these hold: `path` is `.github/workflows/ci.yml`; `status` is `completed` and `conclusion` is `success`, so every job of that run passed, not only `nix`; `event` is `pull_request` or `push`; and `repository.id` and `head_repository.id` both equal this repository's ID. A missing field rejects the candidate.
3. It downloads the artifact ZIP by its ID into `$RUNNER_TEMP`, outside the source checkout, and checks the tarball's sha256 against `provenance.json`.
4. It binds the bytes to main's tree. `GET /git/commits/{commit}` for the recorded commit must return `tree.sha` equal to main's tree. The commit must also belong to the run: it equals the run's `head_sha` on `push`, or lists `head_sha` as a parent on `pull_request`.
5. The first candidate that passes every check is the hit. Any failed check or API error, including a 404 or 410 because the artifact expired or was deleted after the listing, moves on to the next candidate. No passing candidate is a miss.

On a hit, the job unpacks the tarball into `$RUNNER_TEMP`, runs `codesign --verify --strict` and the version check on the unpacked `bin/tess`, and uploads the same tarball as this run's own `tess-darwin-arm64-<tree>` artifact. Its `provenance.json` names the main commit, the digest, and the source run and artifact IDs. On a miss, the job runs Install Nix, flake check, build and the checks as today, then packs and uploads. Either way the main run owns the artifact it publishes, with fresh 90-day retention, so the release job reads only its own run.

**Trust boundary.** These checks prove that the bytes came from a successful same-repo `ci.yml` run that checked out main's tree. They do not authenticate the content. Anyone with write access can change `ci.yml` on a branch, and that run passes the checks. Same-repo writers are therefore trusted, as they already are for main. Fork PRs are not: a fork PR runs its own workflow file and could upload bytes under a predicted tree name, and its `head_repository.id` differs.

**Retention.** "By default … artifacts and log files generated by workflows are retained for 90 days", and public repositories can set "anywhere between 1 day or 90 days" ([Managing GitHub Actions settings](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository#configuring-the-retention-period-for-github-actions-artifacts-and-logs-in-your-repository)). 30 days covers a normal PR. A PR that merges later falls back to the rebuild.

### Release (items 3 and 5)

Publication becomes the last job of the main CI run. `ci.yml` gains a job `release` with `needs: [go, windows, nix]` and `if: github.event_name == 'push' && github.ref == 'refs/heads/main'`. It calls `.github/workflows/release.yml`, whose trigger changes from `push` to `workflow_call`, with `secrets: inherit`. Releases wait for every gate of that run, and the release job downloads the artifact of its own run by name. There is no `workflow_run` trigger and no cross-run artifact selection. The `GITHUB_TOKEN` pin commit starts no new run: "if a workflow run pushes code using the repository's `GITHUB_TOKEN`, a new workflow will not run even when the repository contains a workflow configured to run when `push` events occur" ([Triggering a workflow from a workflow](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)). `[skip ci]` in its subject is a second guard: "Workflows that would otherwise be triggered using `on: push` or `on: pull_request` won't be triggered" ([Skipping workflow runs](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/skip-workflow-runs)).

**What a merge owes (Decision 2).** Every main merge owes a `build-<sha12>` prerelease. A merge also owes `v<VERSION>` when it is the commit that set `VERSION`, that is when `git log -1 --format=%H <sha> -- VERSION` prints `<sha>`. The answer comes from the commit, not from which releases already exist, so a retry reaches the same answer. Two quick bumps each owe their own stable release. Today's `v0.2.0` was cut before this scheme and has no asset; its bump commit is history, so no run owes it. Both releases of one merge carry the same tarball bytes.

**Queue.** Two concurrency changes keep every merge:

- `ci.yml`'s group becomes `${{ github.workflow }}-${{ github.event.pull_request.head.sha || github.sha }}`. Each main push gets its own group, so a newer main run no longer cancels an older one. PR runs keep `cancel-in-progress: true` as today.
- The job in `release.yml` sets `concurrency: { group: release, cancel-in-progress: false, queue: max }`. Without `queue`, "any existing `pending` job or workflow in the same concurrency group will be canceled" ([Control the concurrency of workflows and jobs](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)). With it, groups "queue multiple pending runs and process them sequentially, with support for up to 100 queued jobs or workflow runs per concurrency group", and it needs `cancel-in-progress` false or unset ([GitHub changelog, 2026-05-07](https://github.blog/changelog/2026-05-07-github-actions-concurrency-groups-now-allow-larger-queues)).

Release jobs run one at a time in arrival order. Arrival follows CI completion, not merge order. That is safe: each job publishes its own merge's releases, and only the pin and the cask are ordered, and their guards refuse to move backwards. A release job that fails or is cancelled, including by a full queue, turns that main CI run red. Recovery is a rerun of that run (§ *Recovery*). Matt's merge volume is far below 100 pending releases [INFERENCE: one maintainer plus agents, about a minute per release job].

**The release job.**

1. Download this run's `tess-darwin-arm64-<tree>` into `$RUNNER_TEMP` and check the tarball against `provenance.json`. Record the digest, tag, commit and version in step outputs before any checkout switch.
2. `releasepin publish` for `build-<sha12>`: prerelease, `--latest=false`, notes `Build of <sha>`.
3. If the merge owes a stable release: `releasepin publish` for `v<VERSION>` with generated notes, then W7's GoReleaser step (§ *Windows*).
4. Check out `origin/main`, write `nix/prebuilt.json` naming `build-<sha12>` with `releasepin pin`, and push `chore(release): pin tess build-<sha12> [skip ci]` with `GITHUB_TOKEN`. tessera has no ruleset on main.
5. If the merge owes a stable release: render `Casks/tessera.rb` with `releasepin cask` and push it to the tap with the App token (§ *Tap*).

The pin always names the build tag, because every merge has one. The cask names the stable tag. A release is never deleted once a pin names it, because old pins in consumers' lock files would stop fetching.

**Recovery.** Every stage converges on a target state, so rerunning the release job from its first step is always safe. Each failure turns the main run red; `gh run rerun <run-id> --failed` resumes it.

`releasepin publish` is get-or-create by tag. It always creates a draft first, so it never publishes a release without its asset. `gh release create` alone is not atomic: its steps "use separate API requests and do not form a single atomic operation" ([gh release create](https://cli.github.com/manual/gh_release_create)).

| State found | Action |
| --- | --- |
| No release | Create a draft with `--target <sha>`, upload the asset, then continue as for a draft. |
| Draft | Require `targetCommitish` to equal `<sha>`. Upload the asset if it is absent. If it is present with a different digest, upload with `--clobber`; no pin names a draft. Then publish with `gh release edit <tag> --draft=false` and the tag's flags. |
| Published | Require the tag's commit (`GET /git/ref/tags/<tag>`) to equal `<sha>`. If the asset is absent, upload it. If present, download it and compare digests: equal is done, different fails with both digests. Never `--clobber` a published asset: "existing assets are deleted before new assets are uploaded. If the upload fails, the original assets will be lost" ([gh release upload](https://cli.github.com/manual/gh_release_upload)). |

A draft has no git tag until it is published, so the target is checked on `targetCommitish` before publication and on the tag after. A target mismatch fails in every state.

If the run's artifact has expired by the time of a rerun and the build release already has a published asset, `publish` uses that asset's bytes for the later stages. If no published asset exists, rerun the whole run (`gh run rerun <run-id>`), which repeats reuse or the rebuild. A rebuild can produce different bytes; against a published asset that fails closed, by design.

The pin and tap stages are idempotent. `releasepin pin` exits 3 without writing when the current pin's `rev` already descends from `<sha>`; the job then skips the commit. An unchanged pin file makes no commit. When a push is rejected, the job fetches, resets to `origin/main`, and runs `releasepin pin` again, so the ancestry guard sees the fresh pin. It tries three times. `releasepin cask` exits 3 when the tap's cask already names a higher version, so an out-of-order stable release cannot downgrade the cask. An unchanged cask makes no commit. Tap pushes retry the same way.

**Windows (W7).** `windows-backend/design.md` § *W7* keeps `release.yml` as the place where GoReleaser appends the Windows ZIPs and pushes the Scoop manifest for each new `VERSION`. That step moves into step 3 above, after `v<VERSION>` is published, so the tag exists for GoReleaser's checkout. W7's `gh release create` becomes `releasepin publish`. On a rerun the GoReleaser step is skipped when both Windows ZIPs are on the release. One ZIP alone fails and names the missing one, because a second Windows build would not match the first.

**Tool: hand-written, not GoReleaser.** GoReleaser OSS builds its own binaries, and importing a binary built elsewhere is Pro-only: "This feature is exclusively available with GoReleaser Pro" ([Import pre-built binaries](https://goreleaser.com/customization/builds/builders/prebuilt)). Using it would rebuild on main and break the reuse rule. Its `nix` publisher writes a standalone derivation into a NUR-style repository ("generate and publish a nix derivation to an existing Nix User Repository", [nix publisher](https://goreleaser.com/customization/publish/nix/)), with default path `pkgs/<name>/default.nix`. That file is not wired to this flake's home-manager module, so we would still need glue. Its `homebrew_casks` publisher ([Homebrew Casks](https://goreleaser.com/customization/publish/homebrew_casks/), "Since v2.10") also needs GoReleaser-built archives. The hand-written path is about 60 workflow lines and one Go tool. W7's GoReleaser builds only `windows/*` and appends to the stable release, as § *Windows* says.

### Flake output: where the URL and hash live (item 1)

A flake at commit X cannot contain the hash of the binary built from X. Three options:

| Option | How | Cost |
| --- | --- | --- |
| **A. Bot-committed pin (chosen)** | After release, CI commits `nix/prebuilt.json` (`version`, `rev`, `url`, `hash`) to main. The flake reads it. | One `[skip ci]` commit per merge. With no race, the pin at main's tip names the binary of its parent, whose source is identical. When merges race, the pin names an older merge's binary until the newest release job runs, so the tip's source can differ from the pinned binary for that window. |
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

- **Ad-hoc (Decision 1).** Zero cost and no credentials. The cask `postflight` targets only `#{staged_path}/bin/tess`, not a Homebrew prefix. It runs `/usr/bin/xattr -d com.apple.quarantine` only when `xattr -p com.apple.quarantine` shows the attribute, so an unquarantined reinstall does nothing. Any other failure stops the install. The `caveats` line says the binary is ad-hoc signed, not notarized, and that the bypass circumvents macOS security protections.
- **Developer ID + notarization (later).** Starts when the DUNS number and an Apple Developer Program membership exist, per Matt's ruling. The program has a yearly fee, which is Matt's spend ([Homebrew Casks § Signing and Notarizing](https://goreleaser.com/customization/publish/homebrew_casks/#signing-and-notarizing)). It also needs a Developer ID Application certificate and a notary API key stored as tessera secrets, which goes to **Nemo**. The sign and notarize step runs in the PR build, so main still reuses the exact bytes.

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
- **Artifact key:** `tess-darwin-arm64-<git rev-parse HEAD^{tree}>`, holding `tess-darwin-arm64.tar.gz` and `provenance.json`. Retention 30 days on PR runs, 90 days on main runs. Reused only after the producer-run and tree checks in § *Main reuse*.
- **Bytes shipped = bytes tested:** the build prerelease, the stable release, the pin and the cask of one merge all use the tarball of that merge's own CI run. No release job compiles darwin code.
- **Cadence (Decision 2):** every main merge publishes `build-<sha12>`. The merge that changes `VERSION` also publishes `v<VERSION>`. The pin follows build tags; the cask follows `v` tags only. No main CI run or release job is cancelled by a newer one.
- **Idempotence:** every release stage is get-or-create and verifies target commit and asset digest. A published asset is never replaced.
- **No release deletion** once a pin names it.
- **Toolchain:** Go 1.27.3 for `tools/releasepin`, matching `go.mod` `toolchain go1.27.3`. Existing action majors (`actions/checkout@v4`, `actions/setup-go@v6`).
- **Credentials go to Nemo:** the `zireael-release` App install on tessera, `RELEASE_BOT_APP_ID` and `RELEASE_BOT_PRIVATE_KEY`, and any Apple signing material.
- **Public repo:** name orion only as the downstream consumer. No private paths or hosts in tessera files.
- **Cost:** GitHub-hosted runners and artifact storage on a public repository add no billed meter.
- **Comments:** 1-2 lines saying why. No issue IDs in source.

## Plan

### Task 1: PR build artifact and main reuse (`ci.yml`)

Interfaces:

- Workflow `concurrency.group` becomes `${{ github.workflow }}-${{ github.event.pull_request.head.sha || github.sha }}`; `cancel-in-progress: true` stays.
- `nix` job gains `permissions: { contents: read, actions: read }` and `env: { GH_TOKEN: ${{ github.token }} }`.
- Step `key`: `echo "tree=$(git rev-parse 'HEAD^{tree}')" >> "$GITHUB_OUTPUT"`.
- Steps `Set up Go` (1.27.3) and `Build releasepin`: `go build -o "$RUNNER_TEMP/releasepin" ./tools/releasepin`.
- Step `reuse` (push to main only): `"$RUNNER_TEMP/releasepin" select -repo "$GITHUB_REPOSITORY" -tree "$TREE" -commit "$GITHUB_SHA" -out "$RUNNER_TEMP/out"`. On a hit it leaves the tarball in `-out`, writes `provenance.json` for this run with the source run and artifact IDs, and prints `hit=true`; otherwise `hit=false`. Its output goes to `$GITHUB_OUTPUT`. The job exports `tree` as a job output for the release job.
- Hit path (`if: steps.reuse.outputs.hit == 'true'`): unpack into `$RUNNER_TEMP/check`, then `codesign --verify --strict bin/tess` and the version check on the unpacked binary.
- Miss path (`if: steps.reuse.outputs.hit != 'true'`): Install Nix, Flake check, Build, Verify, `codesign --verify --strict result/bin/tess`, then Pack: `tar -czf "$RUNNER_TEMP/out/tess-darwin-arm64.tar.gz" -C "$(readlink -f result)" bin share` and `releasepin provenance -tarball … -commit "$GITHUB_SHA" -out "$RUNNER_TEMP/out/provenance.json"`.
- Upload (both paths): `actions/upload-artifact@v4`, `path: ${{ runner.temp }}/out`, `name: tess-darwin-arm64-${{ steps.key.outputs.tree }}`, `retention-days: ${{ github.event_name == 'push' && 90 || 30 }}`, `if-no-files-found: error`.

Gate: on a PR, the run shows the artifact and `codesign` passes. After an up-to-date same-repo merge, the main run logs a hit with the source run ID and skips `nix build`. Each of these is a miss that rebuilds: no artifact; the producer run failed or was cancelled (another job red after `nix` uploaded); the artifact came from a workflow other than `ci.yml`; a fork PR's artifact under the same name; a recorded commit whose tree differs; and an artifact deleted between listing and download (simulate with a fake API returning 410). `releasepin select` table tests cover each case.

### Task 2: `tools/releasepin` and the `prebuilt` flake output

Interfaces:

- `tools/releasepin/main.go`, following `tools/benchstartup`. It calls the GitHub API through `gh api` with `GH_TOKEN`, behind an interface that tests fake. Exit 0 is success, 3 is "nothing to do", any other code is failure. Subcommands:
  - `releasepin provenance -tarball PATH -commit SHA [-source-run ID -source-artifact ID] -out PATH`. Writes `type Provenance struct { Commit, SHA256 string; SourceRun, SourceArtifact int64 }` (JSON `commit`, `sha256`, `source_run`, `source_artifact`).
  - `releasepin select -repo OWNER/NAME -tree TREE -commit SHA -out DIR`. The candidate loop, producer-run checks, download, digest check and tree binding of § *Main reuse*. Prints `hit=true` or `hit=false`.
  - `releasepin publish -repo OWNER/NAME -tag TAG -sha SHA -tarball PATH (-prerelease -notes TEXT | -generate-notes)`. The get-or-create table of § *Recovery*. It finds drafts by listing releases and matching `tag_name`, because a draft has no tag yet. On a published release whose asset matches, it exits 0 without changes. With `-tarball` absent and `-fetch-into PATH`, it downloads the published asset instead, for the expired-artifact rerun.
  - `releasepin owes -sha SHA`. Prints `stable=true|false` from `git log -1 --format=%H SHA -- VERSION`, and `version=<VERSION at SHA>`.
  - `releasepin pin -tarball PATH -tag TAG -rev SHA -version V -repo OWNER/NAME -out nix/prebuilt.json`. Exits 3 without writing when the existing pin's `rev` equals `-rev` or descends from it (`git merge-base --is-ancestor -rev <pin rev>`), and when the rendered file equals the existing one.
  - `releasepin cask -tarball PATH -version V -repo OWNER/NAME -out PATH`. Renders `Casks/tessera.rb` with a hex `sha256` of the same tarball, `url` on the `v#{version}` asset, `binary "bin/tess"`, `depends_on arch: :arm64`, the quarantine `postflight` and `caveats`. Exits 3 without writing when the existing cask's version is equal or higher (semver compare).
- `type Pin struct { Version, Rev, URL, Hash string }` with JSON tags `version`, `rev`, `url`, `hash`. `func sriSHA256(b []byte) string` returns `"sha256-" + base64(sha256(b))`.
- `flake.nix`: `pin = builtins.fromJSON (builtins.readFile ./nix/prebuilt.json)`. `packages.prebuilt` as in § *Flake output*, added with `lib.optionalAttrs (system == "aarch64-darwin" && builtins.pathExists ./nix/prebuilt.json)`. The source build's `src` becomes `lib.fileset.toSource { root = ./.; fileset = lib.fileset.difference ./. (lib.fileset.maybeMissing ./nix/prebuilt.json); }`.

Gate: table tests cover pin JSON and SRI for a fixed input, the ancestor and no-change refusals (exit 3), the cask downgrade refusal, a golden cask file, every `select` case in Task 1, and every row of the `publish` table: no release, draft without asset, draft with a different asset, published with equal asset (no-op), published with a different asset (fails, names both digests), published without asset, and a target mismatch in each state. `owes` returns true for a commit that edits `VERSION` and false for its child. `go vet`, `golangci-lint`, `nilaway` and `go test -race ./tools/...` pass. `nix flake check` passes with and without `nix/prebuilt.json`. With a hand-made pin pointing at a test asset, `nix build .#prebuilt` yields a `tess` that prints `tess v0.2.0`.

### Task 3: release workflow

Interfaces:

- `ci.yml` job `release`: `needs: [go, windows, nix]`, `if: github.event_name == 'push' && github.ref == 'refs/heads/main'`, `uses: ./.github/workflows/release.yml`, `secrets: inherit`, `permissions: { contents: write, actions: read }`. A called workflow cannot hold more than its caller job grants, so the write permission lives on this job.
- `release.yml`: `on: workflow_call: { inputs: { tree: { type: string, required: true } } }`. One job with `concurrency: { group: release, cancel-in-progress: false, queue: max }` and `env: { GH_TOKEN: ${{ github.token }} }`.
- Steps: checkout at `github.sha` with `fetch-depth: 0`; setup-go; build `releasepin`; `actions/download-artifact@v4` with `name: tess-darwin-arm64-${{ inputs.tree }}` and `path: ${{ runner.temp }}/rel` (same run, so no `run-id`; the caller passes `with: { tree: ${{ needs.nix.outputs.tree }} }`); `sha256sum` check against `provenance.json`; `releasepin owes`; `releasepin publish` for `build-${GITHUB_SHA:0:12}` (`-prerelease -notes "Build of $GITHUB_SHA"`).
- If `stable=true`: `releasepin publish` for `v<VERSION>` (`-generate-notes`); then W7's GoReleaser step, skipped when both Windows ZIPs are present and failing when only one is.
- Pin: `git checkout -B main origin/main`; `releasepin pin` with the tag, SHA, version and digest captured before the checkout; on exit 0, commit only `nix/prebuilt.json` as `chore(release): pin tess build-<sha12> [skip ci]` and push. On rejection: `git fetch`, `git reset --hard origin/main`, rerun `releasepin pin`, up to 3 tries.
- Tap, if `stable=true`: `actions/create-github-app-token` (jj-hooks inputs, `repositories: homebrew-tap`); checkout `mattwilkinsonn/homebrew-tap` at `main` with that token; `releasepin cask`; on exit 0, commit `chore(tap): bump tessera to v<VERSION>` and push, with the same 3-try loop.
- README gains `## Install`: nix (`packages.<system>.prebuilt` or `programs.tessera.package`), Homebrew (`brew install --cask mattwilkinsonn/tap/tessera`) and the quarantine caveat.

Gate: `actionlint` passes. actionlint 1.7.12 rejects `queue` ([rhysd/actionlint#657](https://github.com/rhysd/actionlint/issues/657)); use a release that accepts it, or ignore exactly that message with `-ignore 'unexpected key "queue"'`. Live checks after Nemo's setup:

- One merge produces a `build-<sha12>` prerelease with the asset, a pin commit that starts no run, and nothing else.
- Three merges within a minute, two of them `VERSION` bumps, produce three build prereleases, both `v` releases with the asset and the Windows ZIPs, a pin naming the newest merge, and a cask naming the higher version. No main CI run shows as cancelled.
- Interruptions, each by cancelling the release job at that point and then running `gh run rerun <run-id> --failed`: after the draft is created; after the asset upload, before publish; after the pin push, before the tap push. Each rerun ends green with the same digests and one pin commit, and the tap gets its commit.
- On a Mac, `brew install --cask mattwilkinsonn/tap/tessera` and `tess --version` succeed on a fresh install, which Homebrew quarantines, and again after a reinstall over an unquarantined file. The installed binary passes `codesign --verify --strict`, and `tess` runs through the installed symlink.

### Task 4: switch Matt's config (orion)

Interfaces: one line, `programs.tessera.package = inputs.tessera.packages.${pkgs.stdenv.hostPlatform.system}.prebuilt;`, after `nix flake update tessera` lands on a commit that has a pin. The existing profile check keeps reading `${config.programs.tessera.package}/share/tessera/schema.cue`.

Gate: a `--dry-run` build of the MacBook's darwin configuration lists no `go-1.27.3`, `tess-*-go-modules` or source `tess-*` derivation to build. Before/after timings of that build on Matt's Mac after a tessera bump are recorded in the PR. The driver runs these as plain `nix build`, without switching.

## Tasks

- [ ] Task 1 — PR build artifact keyed by tree, main reuse with fallback rebuild (`ci.yml`)
- [ ] Task 2 — `tools/releasepin` and the `prebuilt` flake output
- [ ] Task 3 — release job called from main CI: queued, get-or-create build and stable releases, idempotent pin and cask, W7 hook, README (needs Nemo's App install and secrets on tessera)
- [ ] Task 4 — orion: point `programs.tessera.package` at `prebuilt`, with measured timings
