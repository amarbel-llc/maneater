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

# Format code
fmt:
  gofumpt -w .
  goimports -w .
