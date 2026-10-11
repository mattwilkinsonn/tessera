{ pkgs, inputs, ... }:

# Tessera dev shell for the Go build and the remaining TypeScript schema.
# CI uses the same Go and static-analysis tool versions.

let
  goToolchain = inputs.go-overlay.packages.${pkgs.stdenv.system}.go_1_27_2;
  buildGoModule = pkgs.buildGoModule.override { go = goToolchain; };
  golangciLint = goToolchain.tools.golangci-lint."2.14.0";
  govulncheck = goToolchain.tools.govulncheck."1.8.0";
  goLicenses = pkgs.go-licenses.override {
    go = goToolchain;
    inherit buildGoModule;
  };
  nilaway = (buildGoModule {
    pname = "nilaway";
    version = "2026-09-18";
    src = pkgs.fetchFromGitHub {
      owner = "uber-go";
      repo = "nilaway";
      rev = "acb8859b9031bb9496be97e027df5573f9fb5340";
      hash = "sha256-GvDZ5tlvOrTI93tYcIcLd45ZHdqwFopVtoBffD/kbuM=";
    };
    vendorHash = "sha256-qVmvDneq6V/q5UHZ/Cjjqd5/XPPNfvVGoxwg9nz4/Ds=";
    subPackages = [ "cmd/nilaway" ];
  }).overrideAttrs (_: { doCheck = false; });
in
{
  packages = with pkgs; [
    # VCS — jj works colocated with git here.
    jujutsu

    # Docs + workflow linters, matching what CI runs.
    markdownlint-cli2
    actionlint
    goToolchain
    golangciLint
    nilaway
    govulncheck
    goLicenses
    bun
  ];

  enterShell = ''
    echo "tessera dev shell — go $(go version | awk '{print $3}')"
    echo "  direnv exec . gofmt -l ."
    echo "  direnv exec . go vet ./..."
    echo "  direnv exec . go mod tidy -diff"
    echo "  direnv exec . golangci-lint run --config .golangci.yml ./..."
    echo "  direnv exec . nilaway -include-pkgs=github.com/mattwilkinsonn/tessera -test=false ./..."
    echo "  direnv exec . go build ./..."
    echo "  direnv exec . go test -race ./..."
    echo "  direnv exec . go test -race -count=20 ./internal/daemon/... ./internal/effects/..."
    echo "  direnv exec . govulncheck ./..."
    echo "  direnv exec . go-licenses check ./... --allowed_licenses=Apache-2.0,Apache-2.0-WITH-LLVM-exception,MIT,BSD-3-Clause,BSD-2-Clause,ISC,Unicode-3.0,Zlib,MPL-2.0,CC0-1.0 --ignore github.com/mattwilkinsonn/tessera"
    echo "  direnv exec . actionlint"
    echo "  cd schema && bunx tsc --noEmit && bun test"
  '';
}
