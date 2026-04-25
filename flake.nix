{
  description = "Maneater: man page search index and semantic search CLI";

  inputs = {
    nixpkgs.url = "github:amarbel-llc/nixpkgs";
    nixpkgs-master.url = "github:NixOS/nixpkgs/e2dde111aea2c0699531dc616112a96cd55ab8b5";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

    tommy = {
      url = "github:amarbel-llc/tommy";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    bob = {
      url = "github:amarbel-llc/bob";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    madder = {
      url = "github:amarbel-llc/madder";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.utils.follows = "utils";
    };

    purse-first = {
      url = "github:amarbel-llc/purse-first";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-master,
      utils,
      tommy,
      bob,
      madder,
      purse-first,
    }:
    utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ nixpkgs.overlays.default ];
        };

        pkgs-master = import nixpkgs-master {
          inherit system;
        };

        go = pkgs-master.go_1_26;

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

        version = "0.6.1";

        goAppBase = {
          inherit go;
          src = goSrc;
          modules = ./gomod2nix.toml;
          GOTOOLCHAIN = "local";
        };

        maneater-unwrapped = pkgs.buildGoApplication (
          goAppBase
          // {
            pname = "maneater";
            inherit version;
            subPackages = [ "cmd/maneater" ];
            CGO_ENABLED = "1";
            nativeBuildInputs = [ pkgs.pkg-config ];
            buildInputs = [ pkgs.llama-cpp ];
          }
        );

        # maneater-man is the lean companion binary the default manpages
        # corpus spawns per page. No CGO, no llama-cpp, no llama init cost
        # on every subprocess. See maneater#12 / #17.
        maneater-man-unwrapped = pkgs.buildGoApplication (
          goAppBase
          // {
            pname = "maneater-man";
            inherit version;
            subPackages = [ "cmd/maneater-man" ];
            CGO_ENABLED = "0";
          }
        );

        goEnv = pkgs.mkGoEnv {
          pwd = ./.;
          inherit go;
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
          inherit maneater maneater-unwrapped maneater-man-unwrapped;
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
            bob.packages.${system}.batman
            purse-first.packages.${system}.dagnabit
          ];
          MANEATER_TEST_CONFIG = maneater-test-toml;
        };
      }
    );
}
