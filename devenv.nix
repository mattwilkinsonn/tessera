{ pkgs, ... }:

# Tessera dev shell — the single source of the dev + CI toolchain for this
# single-tool repo. Deliberately minimal: Tessera is one zero-dependency
# Bun/TypeScript CLI (`tess`), so the shell is just Bun + Biome + the VCS and
# docs linters. No monorepo build system — `bun test` / `bun run typecheck` /
# `bun run check` ARE the gate, run directly and by CI (.github/workflows/ci.yml).

{
  packages = with pkgs; [
    # VCS — jj (Matt's review tool) works colocated with git here.
    jujutsu

    # Docs + workflow linters, matching what CI runs.
    markdownlint-cli2
    actionlint
  ];

  languages.javascript = {
    enable = true;
    # Bun is the runtime, package manager, test runner, and bundler. Biome and
    # tsc come from devDependencies (pinned in package.json), installed by the
    # `bun install` below — kept in the lockfile so dev and CI resolve the same
    # versions.
    bun = {
      enable = true;
      install.enable = true;
    };
  };

  enterShell = ''
    echo "tessera dev shell — bun $(bun --version)"
    echo "  bun test          run the suite"
    echo "  bun run typecheck  tsc --noEmit"
    echo "  bun run check      biome check src/"
  '';
}
