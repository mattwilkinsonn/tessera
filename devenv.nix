{ pkgs, inputs, ... }:

# Tessera dev shell — the single source of the Bun/TypeScript and Go toolchains.
# CI uses the same Go version and linter release.

{
  packages = with pkgs; [
    bun2nix
    # VCS — jj (Matt's review tool) works colocated with git here.
    jujutsu

    # Docs + workflow linters, matching what CI runs.
    markdownlint-cli2
    actionlint
    go_1_27
    golangci-lint

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
