{
  description = "Tessera — a portable TypeScript/Bun window-manager config (CLI: tess)";

  # Self-pinned inputs (the fork-owns-its-inputs pattern): tessera pins its own
  # nixpkgs + bun so the flake is reproducible standalone. A consumer that wants
  # to share its own channel drops the pin with `inputs.tessera.inputs.nixpkgs.
  # follows = "…"` (orion does exactly this — see the tessera-oss-repoint design,
  # Fork 1 / Q9). bun rides nixpkgs, so there is no separate bun input to pin.
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    let
      # homeManagerModules.default and overlays.default are system-agnostic and
      # live outside eachSystem; the overlay resolves the package for whatever
      # system the consumer's pkgs set targets.
      overlay = final: prev: {
        tess = self.packages.${prev.system}.default;
      };

      # home-manager module — R1's consumption contract. Option names are load
      # bearing (a downstream orion module sets them): programs.tessera.{ enable,
      # package, profilePath }. Enabling puts `tess` on PATH and records the
      # profile path the binary loads at runtime; it does NOT own or render the
      # profile content (that stays a private orion project).
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
              default = self.packages.${pkgs.system}.default;
              defaultText = lib.literalExpression "tessera.packages.\${system}.default";
              description = "The tessera package providing the `tess` binary.";
            };

            profilePath = lib.mkOption {
              type = lib.types.str;
              # MUST be absolute: the value is exported verbatim as
              # $TESSERA_PROFILE and the binary's loader does NOT expand `~`
              # (a leading tilde reaches Bun's import() unexpanded and crashes).
              default = "${config.home.homeDirectory}/.config/tessera/profile.ts";
              defaultText = lib.literalExpression ''"''${config.home.homeDirectory}/.config/tessera/profile.ts"'';
              description = ''
                Absolute path the `tess` binary loads its profile from at runtime,
                exported as $TESSERA_PROFILE. This module only points at the
                deployed path; it does not own or render the profile content.

                Note the delivery channel: this is set via `home.sessionVariables`,
                which reaches interactive/login shells but NOT launchd-started
                daemons (skhd/yabai invoke `tess` from launchd). A daemon-invoked
                `tess` therefore relies on the binary's own well-known fallback
                (`$XDG_CONFIG_HOME/tessera/profile.ts`, else `~/.config/...`); keep
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

        # The darwin `bun build --compile` recipe, moved verbatim from orion's
        # darwin/wm.nix (renamed wm → tess). Every attr here is load-bearing;
        # see the comments for what each one prevents.
        tess = pkgs.stdenvNoCC.mkDerivation {
          pname = "tess";
          inherit (pkgs.lib.importJSON ./package.json) version;
          src = ./.;

          nativeBuildInputs = [
            pkgs.bun
          ]
          ++ pkgs.lib.optionals pkgs.stdenv.hostPlatform.isDarwin [
            # A compiled arm64 Mach-O must carry an ad-hoc code signature or
            # Gatekeeper kills it. BUN_NO_CODESIGN_MACHO_BINARY=1 tells bun NOT
            # to sign, and this fixup hook ad-hoc-signs the emitted binary.
            pkgs.darwin.autoSignDarwinBinariesHook
          ];

          env =
            pkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isDarwin {
              BUN_NO_CODESIGN_MACHO_BINARY = "1";
            }
            // {
              SOURCE_DATE_EPOCH = "1";
            };

          dontConfigure = true;
          # bun --compile appends the JS bundle as a trailer after the Mach-O; a
          # strip pass would corrupt it.
          dontStrip = true;

          # A `bun --compile` standalone embeds its runtime, so the binary must
          # not retain a store reference to the ~90MB bun package — assert the
          # clean closure; a green build alone would not catch a leak.
          disallowedReferences = [ pkgs.bun ];

          buildPhase = ''
            runHook preBuild
            export HOME=$(mktemp -d)
            ${pkgs.bun}/bin/bun build --compile src/index.ts --outfile tess
            runHook postBuild
          '';

          installPhase = ''
            runHook preInstall
            install -Dm755 tess "$out/bin/tess"
            runHook postInstall
          '';

          # Enforce the parse-failure smoke in-build so it is reproducible on
          # every rebuild: the entry guard exits 2 on a bad subcommand.
          doInstallCheck = true;
          installCheckPhase = ''
            runHook preInstallCheck
            rc=0
            HOME="$TMPDIR" "$out/bin/tess" bogus-subcommand || rc=$?
            test "$rc" -eq 2
            runHook postInstallCheck
          '';

          meta = {
            mainProgram = "tess";
            # darwin-only for now; the Hyprland driver later widens this.
            platforms = pkgs.lib.platforms.darwin;
          };
        };
      in
      {
        packages.default = tess;

        # Guard the home-manager module's cross-repo contract: evaluate it with
        # stub home.* options and assert the exported TESSERA_PROFILE is an
        # ABSOLUTE path (a leading `~` reaches the binary's import() unexpanded
        # and crashes `tess apply`). `nix build .#default` + the package's
        # bogus-subcommand smoke both exit before the loader runs, so this is
        # the only gate that exercises the profile-path default.
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
            isAbsolute = pkgs.lib.hasPrefix "/" profileVar;
          in
          assert isAbsolute;
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
