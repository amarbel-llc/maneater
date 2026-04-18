#! /bin/bash -e

if [[ -z $BATS_TEST_TMPDIR ]]; then
  echo 'common.bash loaded before $BATS_TEST_TMPDIR set. aborting.' >&2

  cat >&2 <<-'EOM'
    only load this file from `.bats` files like so:

    setup() {
      load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"

      # for shellcheck SC2154
      export output
    }

    as there is a hard assumption on $BATS_TEST_TMPDIR being set
EOM

  exit 1
fi

pushd "$BATS_TEST_TMPDIR" >/dev/null || exit 1

bats_load_library bats-support
bats_load_library bats-assert
bats_load_library bats-emo
bats_load_library bats-island

setup_test_home
# Stop madder from walking above $BATS_TEST_TMPDIR when it probes for a
# workspace config. Undocumented but present upstream as
# `<UTILITY>_CEILING_DIRECTORIES` (lib/echo/xdg/main.go).
export MADDER_CEILING_DIRECTORIES="$BATS_TEST_TMPDIR"
require_bin MANEATER_BIN maneater

run_maneater() {
  local bin="${MANEATER_BIN:-maneater}"
  run timeout --preserve-status 60s "$bin" "$@" 2>&1
}

init_maneater_store() {
  run_maneater init-store
  assert_success
}

write_test_config() {
  if [[ -z ${MANEATER_TEST_CONFIG:-} ]]; then
    skip "MANEATER_TEST_CONFIG not set (run inside nix devshell)"
  fi

  local fixtures_dir="$BATS_TEST_TMPDIR/fixtures"
  mkdir -p "$fixtures_dir"

  local repo_root
  repo_root="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"

  cp "$repo_root/cmd/maneater/maneater.1" "$fixtures_dir/maneater.1"
  cp "$repo_root/cmd/maneater/maneater.toml.5" "$fixtures_dir/maneater.toml.5"

  local config_dir="$HOME/.config/maneater"
  mkdir -p "$config_dir"
  cat >"$config_dir/maneater.toml" <<EOF
[[corpora]]
name = "test-docs"
type = "files"
paths = ["$fixtures_dir/*"]
max-chars = 500
EOF

  export MANEATER_CONFIG="$MANEATER_TEST_CONFIG"
}
