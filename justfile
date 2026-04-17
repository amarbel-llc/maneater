# Build and test
default: build test

# Build maneater binary
build: generate
  go build -o build/maneater ./cmd/maneater

# Run all tests
test: fmt
  go test ./...

# Run go generate (regenerate config_tommy.go)
generate:
  go generate ./cmd/maneater/

# Regenerate gomod2nix.toml
gomod2nix:
  gomod2nix

# Build nix package
build-nix:
  nix build --show-trace

[group('explore')]
man-tree:
  mkdir -p build/man/man1 build/man/man5
  ln -sf ../../../cmd/maneater/maneater.1 build/man/man1/maneater.1
  ln -sf ../../../cmd/maneater/maneater.toml.5 build/man/man5/maneater.toml.5

# Format code
fmt:
  gofumpt -w .
  goimports -w .
