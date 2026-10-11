{ pkgs, ... }:

# Tessera dev shell for the Go build and the remaining TypeScript schema.
# CI uses the same Go version and linter release.

{
  packages = with pkgs; [
    # VCS — jj (Matt's review tool) works colocated with git here.
    jujutsu

    # Docs + workflow linters, matching what CI runs.
    markdownlint-cli2
    actionlint
    go_1_27
    golangci-lint
    bun
  ];

  enterShell = ''
    echo "tessera dev shell — go $(go version | awk '{print $3}')"
    echo "  direnv exec . go vet ./..."
    echo "  direnv exec . golangci-lint run ./..."
    echo "  direnv exec . go test -race ./..."
    echo "  cd schema && bunx tsc --noEmit && bun test"
  '';
}
