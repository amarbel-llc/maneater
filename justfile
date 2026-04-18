# Build and test
default: build test test-bats

# Build maneater binary
build: generate
  go build -o build/maneater ./cmd/maneater

# Run all tests
test: fmt
  go test ./...

# Run go generate (regenerate config_tommy.go)
generate:
  go generate ./...

# Regenerate gomod2nix.toml
gomod2nix:
  gomod2nix

# Build nix package
build-nix:
  nix build --show-trace

# Build the wrapped maneater (madder + mandoc + pandoc + tldr on its PATH)
build-wrapped:
  nix build --out-link build/result-wrapped .#default

[group('explore')]
man-tree:
  mkdir -p build/man/man1 build/man/man5
  ln -sf ../../../cmd/maneater/maneater.1 build/man/man1/maneater.1
  ln -sf ../../../cmd/maneater/maneater.toml.5 build/man/man5/maneater.toml.5

# Run bats integration tests (against the wrapped binary so madder is on its PATH)
[group('test')]
test-bats: build-wrapped
  MANEATER_BIN={{justfile_directory()}}/build/result-wrapped/bin/maneater bats --no-sandbox zz-tests_bats/

# Format code
fmt:
  gofumpt -w .
  goimports -w .
