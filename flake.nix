{
  description = "Tessera — a portable Go window-manager config (CLI: tess)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    go-overlay = {
      url = "github:purpleclay/go-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      go-overlay,
    }:
    let
      overlay = final: prev: {
        tess = self.packages.${prev.stdenv.hostPlatform.system}.default;
      };

      hmModule =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        let
          cfg = config.programs.tessera;
        in
        {
          options.programs.tessera = {
            enable = lib.mkEnableOption "the tessera window-manager CLI (tess)";

            package = lib.mkOption {
              type = lib.types.package;
              default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
              defaultText = lib.literalExpression "tessera.packages.\${system}.default";
              description = "The tessera package providing the `tess` binary.";
            };

            profilePath = lib.mkOption {
              type = lib.types.str;
              # The profile path is exported verbatim and must be absolute.
              default = "${config.home.homeDirectory}/.config/tessera/profile.json";
              defaultText = lib.literalExpression ''"''${config.home.homeDirectory}/.config/tessera/profile.json"'';
              description = ''
                Absolute path the `tess` binary loads its profile from at runtime,
                exported as $TESSERA_PROFILE. This module only points at the
                deployed path; it does not own or render the profile content.

                Note the delivery channel: this is set via `home.sessionVariables`,
                which reaches interactive/login shells but NOT launchd-started
                daemons (skhd/yabai invoke `tess` from launchd). A daemon-invoked
                `tess` therefore relies on the binary's own well-known fallback
                (`$XDG_CONFIG_HOME/tessera/profile.json`, else `~/.config/...`); keep
                `profilePath` at the default so both channels resolve the same
                file. A non-default path is honored only for shell-invoked `tess`.
              '';
            };
          };

          config = lib.mkIf cfg.enable {
            home.packages = [ cfg.package ];
            home.sessionVariables.TESSERA_PROFILE = cfg.profilePath;
          };
        };
    in
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        goToolchain = go-overlay.packages.${system}.go_1_27_2;
        version = pkgs.lib.fileContents ./VERSION;
        yabaiStub = pkgs.writeShellScript "tessera-yabai-stub" ''
          printf '[]\n'
        '';
        tess = (pkgs.buildGoModule.override { go = goToolchain; }) {
          pname = "tess";
          inherit version;
          src = ./.;
          vendorHash = "sha256-acNjymY5I170LrcrshhRk740oZ1LCwFv4lEjKO6qiQk=";
          env.CGO_ENABLED = "0";
          ldflags = [ "-X main.version=${version}" ];
          go = goToolchain;
          allowGoReference = false;
          doCheck = false;
          doInstallCheck = true;
          installCheckPhase = ''
            runHook preInstallCheck
            rc=0
            HOME="$TMPDIR" "$out/bin/tess" bogus-subcommand >out 2>err || rc=$?
            test "$rc" -eq 1
            rc=0
            HOME="$TMPDIR" "$out/bin/tess" --help >out 2>err || rc=$?
            test "$rc" -eq 0
            HOME="$TMPDIR" "$out/bin/tess" --version >out 2>err
            grep -q "tess v$version" out
            rc=0
            HOME="$TMPDIR" TESSERA_PROFILE= XDG_CONFIG_HOME="$TMPDIR/config" TESS_YABAI=${yabaiStub} "$out/bin/tess" display-setup >out 2>err || rc=$?
            test "$rc" -eq 0
            runHook postInstallCheck
          '';

          meta = {
            mainProgram = "tess";
            platforms = pkgs.lib.platforms.darwin;
          };
        };
      in
      {
        packages.default = tess;

        checks.default =
          let
            eval = pkgs.lib.evalModules {
              specialArgs = { inherit pkgs; };
              modules = [
                hmModule
                {
                  options.home = {
                    homeDirectory = pkgs.lib.mkOption { type = pkgs.lib.types.str; };
                    packages = pkgs.lib.mkOption {
                      type = pkgs.lib.types.listOf pkgs.lib.types.package;
                      default = [ ];
                    };
                    sessionVariables = pkgs.lib.mkOption {
                      type = pkgs.lib.types.attrsOf pkgs.lib.types.str;
                      default = { };
                    };
                  };
                  config = {
                    home.homeDirectory = "/Users/example";
                    programs.tessera.enable = true;
                  };
                }
              ];
            };
            profileVar = eval.config.home.sessionVariables.TESSERA_PROFILE;
            expected = "/Users/example/.config/tessera/profile.json";
            isAbsolute = pkgs.lib.hasPrefix "/" profileVar;
          in
          assert isAbsolute;
          assert profileVar == expected;
          pkgs.runCommand "tessera-hm-module-check" { } ''
            echo "TESSERA_PROFILE=${profileVar} is absolute" > "$out"
          '';
      }
    )
    // {
      homeManagerModules.default = hmModule;
      overlays.default = overlay;
    };
}
