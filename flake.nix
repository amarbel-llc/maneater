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

        nomic-model = pkgs.fetchurl {
          url = "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf";
          hash = "sha256-PiQ0IWSz2UmRupaS/cDdCOP9c2Lgqsw5appcVKVEw7c=";
        };

        snowflake-model = pkgs.fetchurl {
          url = "https://huggingface.co/Casual-Autopsy/snowflake-arctic-embed-l-v2.0-gguf/resolve/main/snowflake-arctic-embed-l-v2.0-q8_0.gguf";
          hash = "sha256-C+gyDssPtuIF8KFBnOPUaINLxE0Cy/2l/RcbNoGxJZc=";
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

          [[corpora]]
          type = "manpages"
        '';

        maneater-unwrapped = pkgs.buildGoApplication {
          pname = "maneater";
          version = "0.6.0";
          src = ./.;
          subPackages = [ "cmd/maneater" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          CGO_ENABLED = "1";
          nativeBuildInputs = [ pkgs.pkg-config ];
          buildInputs = [ pkgs.llama-cpp ];
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
                  ]
                } \
                --set-default MANEATER_CONFIG ${maneater-base-toml}
              ${maneater-unwrapped}/bin/maneater generate-plugin $out
            '';
      in
      {
        packages = {
          inherit maneater maneater-unwrapped;
          default = maneater;
        };

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
          ];
          MANEATER_TEST_CONFIG = maneater-test-toml;
        };
      }
    );
}
