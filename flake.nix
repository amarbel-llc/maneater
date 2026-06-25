{
  description = "Maneater: man page search index and semantic search CLI";

  inputs = {
    igloo.url = "github:amarbel-llc/igloo";
    nixpkgs-master.url = "github:NixOS/nixpkgs/567a49d1913ce81ac6e9582e3553dd90a955875f";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

    tap = {
      url = "github:amarbel-llc/tap";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    tommy = {
      url = "github:amarbel-llc/tommy";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    bats = {
      url = "github:amarbel-llc/bats";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    madder = {
      url = "github:amarbel-llc/madder";
      inputs.igloo.follows = "igloo";
      inputs.utils.follows = "utils";
    };

    purse-first = {
      url = "github:amarbel-llc/purse-first";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
  };

  outputs =
    {
      self,
      igloo,
      nixpkgs-master,
      utils,
      tap,
      tommy,
      bats,
      madder,
      purse-first,
    }:
    let
      # version.env at repo root is the single source of truth for the
      # release version. Burnt into the binary via the fork's
      # auto-injected -ldflags (-X main.version / -X main.commit).
      maneaterVersion = builtins.head (builtins.match
        ".*MANEATER_VERSION=([^\n]+).*"
        (builtins.readFile ./version.env));
      # shortRev for clean builds, dirtyShortRev for dirty working trees
      # (so devshell builds show `dirty-abcdef` rather than masquerading
      # as a clean release), "unknown" as a last-resort fallback.
      maneaterCommit = self.shortRev or self.dirtyShortRev or "unknown";
    in
    utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import igloo {
          inherit system;
        };

        pkgs-master = import nixpkgs-master {
          inherit system;
        };

        go = pkgs-master.go_1_26;

        # flake-input-go_mod consumer-half bridge. See gomod.nix and
        # amarbel-llc/nixpkgs RFC 0001. Threaded into every
        # buildGoApplication and mkGoEnv call below; a missing call
        # site silently falls back to organic gomod2nix.toml resolution
        # and resurrects the lockstep regression.
        goFlakeInputs = import ./gomod.nix {
          inherit tap tommy purse-first system;
        };

        snowflake-model = pkgs.fetchGgufModel {
          name = "snowflake-arctic-embed-l-v2.0-q8_0";
          url = "https://huggingface.co/Casual-Autopsy/snowflake-arctic-embed-l-v2.0-gguf/resolve/main/snowflake-arctic-embed-l-v2.0-q8_0.gguf";
          sha256 = "sha256-C+gyDssPtuIF8KFBnOPUaINLxE0Cy/2l/RcbNoGxJZc=";
        };

        # Smart-retrieval profile reference model. See
        # docs/features/0001-smart-retrieval-corpus-profile.md.
        qwen3-embedding-4b-model = pkgs.fetchGgufModel {
          name = "qwen3-embedding-4b-q8_0";
          url = "https://huggingface.co/Qwen/Qwen3-Embedding-4B-GGUF/resolve/main/Qwen3-Embedding-4B-Q8_0.gguf";
          sha256 = "sha256-tgrlzi3WoLd/gsrfId7x8xCj4QzeOArQCBsHqdQWlJ0=";
        };

        maneaterTomls = import ./maneater-toml.nix {
          inherit pkgs snowflake-model qwen3-embedding-4b-model;
        };
        maneater-test-toml = maneaterTomls.test;
        maneater-base-toml = maneaterTomls.base;

        # Exclude non-Go-source paths so edits to docs, tests, justfile, etc.
        # don't bust the derivation hash and trigger a full CGO rebuild.
        goSrc = pkgs.lib.cleanSourceWith {
          src = ./.;
          filter =
            path: _type:
            !(pkgs.lib.hasSuffix "/justfile" path)
            && !(pkgs.lib.hasSuffix "/sweatfile" path)
            && !(pkgs.lib.hasSuffix "/AGENTS.md" path)
            && !(pkgs.lib.hasSuffix "/README.md" path)
            && !(pkgs.lib.hasInfix "/docs/" path)
            && !(pkgs.lib.hasInfix "/zz-tests_bats/" path)
            && !(pkgs.lib.hasInfix "/zz-fixtures/" path)
            && !(pkgs.lib.hasInfix "/build/" path)
            && !(pkgs.lib.hasInfix "/.tmp/" path);
        };

        goAppBase = {
          inherit go goFlakeInputs;
          src = goSrc;
          pwd = ./.;
          modules = ./gomod2nix.toml;
          GOTOOLCHAIN = "local";
          version = maneaterVersion;
          commit = maneaterCommit;
        };

        maneater-unwrapped = pkgs.buildGoApplication (
          goAppBase
          // {
            pname = "maneater";
            subPackages = [ "cmd/maneater" ];
            CGO_ENABLED = "1";
            nativeBuildInputs = [ pkgs.pkg-config ];
            buildInputs = [ pkgs.llama-cpp ];
            # llama-cpp ships its compute backends (libggml-cpu-*.so,
            # libggml-metal.so) as separate dynamic libraries under
            # ${llama-cpp}/bin. ggml_backend_load_all() only scans the
            # running binary's own directory, which in the nix layout
            # does not contain them, so without this the model load
            # fails with "no backends are loaded". The -D points
            # internal/0/embedding's loader at the right directory; see
            # backend_init.go.
            CGO_CFLAGS = "-DMANEATER_GGML_BACKEND_DIR=\"${pkgs.llama-cpp}/bin\"";
            # Point the embedding tests at the snowflake FOD so the
            # checkPhase exercises real model loading and inference
            # instead of skipping (the tests skip when MANPAGE_MODEL_PATH
            # is unset). Referencing the FOD pulls it into the sandbox.
            # Cross-platform: llama-cpp builds with GGML_BACKEND_DL on
            # every platform and ships a CPU backend .so in bin/ (Metal
            # is darwin-only), so CPU inference runs headless in both the
            # darwin and linux sandboxes. See backend_init.go.
            MANPAGE_MODEL_PATH = snowflake-model;
            # checkPhase mirrors madder/go/default.nix:159-163. The default
            # goCheckHook only tests subPackages (cmd/* dirs with no tests);
            # this override runs the full unit-test surface inside the
            # build sandbox. CGO + llama-cpp are already in buildInputs.
            doCheck = true;
            checkPhase = ''
              runHook preCheck
              go test -p $NIX_BUILD_CORES ./...
              runHook postCheck
            '';
          }
        );

        # maneater-man is the lean companion binary the default manpages
        # corpus spawns per page. No CGO, no llama-cpp, no llama init cost
        # on every subprocess. See maneater#12 / #17.
        maneater-man-unwrapped = pkgs.buildGoApplication (
          goAppBase
          // {
            pname = "maneater-man";
            subPackages = [ "cmd/maneater-man" ];
            CGO_ENABLED = "0";
            # maneater-unwrapped's checkPhase already runs the full Go
            # suite; re-running it here would double the test cost for
            # every default build.
            doCheck = false;
          }
        );

        # maneater-gen runs `go generate` against the source tree inside
        # a nix sandbox and emits the generated schema_tommy.go. The
        # justfile `generate` recipe copies the result back into
        # internal/0/config/schema/. Keeps `go generate` out of host-side
        # justfile recipes.
        #
        # Piggybacks on maneater-man-unwrapped so the gomod2nix vendor
        # cache is already wired up (`tommy generate` imports the tommy
        # CST package and would otherwise try to fetch modules over the
        # network, which the build sandbox forbids). Build/install phases
        # are replaced; we don't ship the cmd/maneater-man binary from
        # this derivation.
        maneater-gen = maneater-man-unwrapped.overrideAttrs (old: {
          pname = "maneater-gen";
          nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [
            tommy.packages.${system}.default
            pkgs-master.gofumpt
            pkgs-master.gotools # provides goimports
          ];
          # After codegen, run goimports + gofumpt so the emitted
          # schema_tommy.go matches what the `fmt` recipe would produce;
          # otherwise `just generate` then `just test` would dirty the
          # working tree with formatting diffs the user didn't request.
          buildPhase = ''
            runHook preBuild
            go generate ./internal/0/config/schema
            goimports -w internal/0/config/schema/schema_tommy.go
            gofumpt -w internal/0/config/schema/schema_tommy.go
            runHook postBuild
          '';
          installPhase = ''
            runHook preInstall
            mkdir -p $out
            cp internal/0/config/schema/schema_tommy.go $out/
            runHook postInstall
          '';
        });

        goEnv = pkgs.mkGoEnv {
          pwd = ./.;
          inherit go goFlakeInputs;
        };

        maneater =
          pkgs.runCommand "maneater-wrapped"
            {
              nativeBuildInputs = [ pkgs.makeWrapper ];
            }
            ''
              mkdir -p $out/bin
              makeWrapper ${maneater-unwrapped}/bin/maneater $out/bin/maneater \
                --prefix PATH : ${
                  pkgs.lib.makeBinPath [
                    pkgs.mandoc
                    pkgs.pandoc
                    pkgs.tldr
                    go
                    madder.packages.${system}.default
                    maneater-man-unwrapped
                  ]
                } \
                --set-default MANEATER_CONFIG ${maneater-base-toml}
              ${maneater-unwrapped}/bin/maneater generate-plugin $out
            '';
      in
      {
        packages = {
          inherit
            maneater
            maneater-unwrapped
            maneater-man-unwrapped
            maneater-gen
            ;
          default = maneater;
        };

        devShells.default = pkgs-master.mkShell {
          packages = [
            goEnv
            pkgs-master.gopls
            pkgs-master.gotools
            pkgs-master.golangci-lint
            pkgs-master.delve
            pkgs-master.gofumpt
            pkgs.just
            pkgs.llama-cpp
            pkgs.pandoc
            pkgs.pkg-config
            pkgs.ripgrep
            tommy.packages.${system}.default
            madder.packages.${system}.default
            bats.packages.${system}.bats
            bats.packages.${system}.batman
            purse-first.packages.${system}.dagnabit
          ];
          MANEATER_TEST_CONFIG = maneater-test-toml;
          # Mirror maneater-unwrapped's CGO_CFLAGS so devshell `go test`
          # / `go build` resolve llama-cpp's backend dylibs the same way
          # the nix build does. Without it the embedding loader falls
          # back to the executable-dir scan and fails with "no backends
          # are loaded". See backend_init.go.
          CGO_CFLAGS = "-DMANEATER_GGML_BACKEND_DIR=\"${pkgs.llama-cpp}/bin\"";
        };
      }
    );
}
