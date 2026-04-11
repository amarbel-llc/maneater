# Build maneater binary
build:
  go build -o build/maneater ./cmd/maneater

# Run all tests
test:
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
