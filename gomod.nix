# Nix interface to go.mod for maneater. Pure-consumer half of the
# flake-input-go_mod protocol (amarbel-llc/nixpkgs RFC 0001).
#
# Routes the cross-amarbel-llc go.mod `require` lines onto their
# producer flakes' go-pkgs outputs, collapsing the three-place
# lockstep (go.mod pseudo-version + gomod2nix.toml NAR hash + flake.lock
# rev) into a single source: each producer's flake.lock entry.
#
# Threaded through every buildGoApplication and mkGoEnv call site in
# flake.nix — a missing call site silently resurrects the lockstep
# regression class (amarbel-llc/madder#208 / #211 / #213).
{
  tap,
  tommy,
  purse-first,
  purse-first-dewey-legacy,
  system,
}:
{
  "github.com/amarbel-llc/tap/go" = {
    src = tap.packages.${system}.go-pkgs;
    subPath = "go";
  };
  "code.linenisgreat.com/tommy" = {
    src = tommy.packages.${system}.go-pkgs;
  };
  "code.linenisgreat.com/purse-first/libs/go-mcp" = {
    src = purse-first.packages.${system}.go-pkgs;
    subPath = "libs/go-mcp";
  };
  # maneater's indirect dewey require stays on the pre-rename
  # github.com/amarbel-llc/purse-first/libs/dewey path (wave-3-B1
  # residue — dewey's own rename is deferred to wave 4; it reaches
  # maneater transitively through tap, which hasn't migrated either).
  # Bridging it through the root `purse-first` input (now renamed, for
  # go-mcp) would serve mismatched content: an old-path require
  # satisfied by a package that internally declares the new path.
  # Sourced from a decoupled pre-rename purse-first snapshot instead.
  "github.com/amarbel-llc/purse-first/libs/dewey" = {
    src = purse-first-dewey-legacy.packages.${system}.go-pkgs;
    subPath = "libs/dewey";
  };
}
