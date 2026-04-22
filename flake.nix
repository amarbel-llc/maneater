{
  description = "Maneater: man page search index and semantic search CLI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/4590696c8693fea477850fe379a01544293ca4e2";
    nixpkgs-master.url = "github:NixOS/nixpkgs/e2dde111aea2c0699531dc616112a96cd55ab8b5";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

    gomod2nix = {
      url = "github:amarbel-llc/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };

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
      gomod2nix,
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
          overlays = [
            gomod2nix.overlays.default
          ];
        };

        pkgs-master = import nixpkgs-master {
          inherit system;
        };

        # Fetch a GGUF embedding model by name, URL, and sha256. The sha256 may
        # be in any format builtins.convertHash accepts: hex, base16, base32,
        # base64, or SRI (sha256-<base64>). HuggingFace shows hex on file pages.
        fetchGgufModel =
          { name, url, sha256 }:
          pkgs.fetchurl {
            inherit url;
            name = "${name}.gguf";
            hash = builtins.convertHash {
              hash = sha256;
              hashAlgo = "sha256";
              toHashFormat = "sri";
            };
          };

        nomic-model = fetchGgufModel {
          name = "nomic-embed-text-v1.5-Q8_0";
          url = "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf";
          sha256 = "sha256-PiQ0IWSz2UmRupaS/cDdCOP9c2Lgqsw5appcVKVEw7c=";
        };

        snowflake-model = fetchGgufModel {
          name = "snowflake-arctic-embed-l-v2.0-q8_0";
          url = "https://huggingface.co/Casual-Autopsy/snowflake-arctic-embed-l-v2.0-gguf/resolve/main/snowflake-arctic-embed-l-v2.0-q8_0.gguf";
          sha256 = "sha256-C+gyDssPtuIF8KFBnOPUaINLxE0Cy/2l/RcbNoGxJZc=";
        };

        maneater-test-toml = pkgs.writeText "maneater-test.toml" ''
          [models.snowflake]
          path = "${snowflake-model}"
          query-prefix = "query: "
          document-prefix = ""
        '';

        maneater-base-toml = pkgs.writeText "maneater.toml" ''
          default = "snowflake"

          [models.nomic]
          path = "${nomic-model}"
          query-prefix = "search_query: "
          document-prefix = "search_document: "

          [models.snowflake]
          path = "${snowflake-model}"
          query-prefix = "query: "
          document-prefix = ""

          # No [[corpora]] entries: maneater's synthesized default
          # activates a `type = "command"` manpages corpus that shells
          # out to maneater-man. See internal/charlie/commands.defaultManpagesCorpusConfig.
        '';

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

        maneater-unwrapped = pkgs.buildGoApplication {
          pname = "maneater";
          version = "0.6.0";
          src = goSrc;
          subPackages = [ "cmd/maneater" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          CGO_ENABLED = "1";
          nativeBuildInputs = [ pkgs.pkg-config ];
          buildInputs = [ pkgs.llama-cpp ];
        };

        # maneater-man is the lean companion binary the default manpages
        # corpus spawns per page. No CGO, no llama-cpp, no llama init cost
        # on every subprocess. See maneater#12 / #17.
        maneater-man-unwrapped = pkgs.buildGoApplication {
          pname = "maneater-man";
          version = "0.6.0";
          src = goSrc;
          subPackages = [ "cmd/maneater-man" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          CGO_ENABLED = "0";
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
                    pkgs-master.go_1_26
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

        lib = { inherit fetchGgufModel; };

        devShells.default = pkgs-master.mkShell {
          packages = [
            pkgs-master.go_1_26
            pkgs-master.gopls
            pkgs-master.gotools
            pkgs-master.golangci-lint
            pkgs-master.delve
            pkgs-master.gofumpt
            gomod2nix.packages.${system}.default
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
