#!/usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=pipeline,e2e

function init_store_succeeds { # @test
  init_maneater_store
}

function index_without_store_fails_fast { # @test
  write_test_config
  # deliberately NOT calling init_maneater_store

  run_maneater index
  assert_failure
  assert_output --partial "init-store"
  refute_output --partial "Using model"
}

function index_builds_with_files_corpus { # @test
  write_test_config
  init_maneater_store

  run_maneater index
  assert_success
  assert_output --partial "Done: test-docs"
  assert_output --partial "entries"
}

function search_returns_results { # @test
  write_test_config
  init_maneater_store

  run_maneater index
  assert_success

  run_maneater search "semantic search"
  assert_success
  assert_output --partial "maneater"
}

function search_without_index_prints_guidance { # @test
  write_test_config
  init_maneater_store

  run_maneater search "anything"
  assert_output --partial "no index"
}

function index_is_incremental { # @test
  write_test_config
  init_maneater_store

  run_maneater index
  assert_success

  run_maneater index
  assert_success
  assert_output --partial "from blob store"
}

function force_rebuild_reembeds_all { # @test
  write_test_config
  init_maneater_store

  run_maneater index
  assert_success

  run_maneater index --force
  assert_success
  refute_output --partial "from blob store"
}

# Configure a filesystem-backed custom storage: read/write/exists/init
# all shell out to sh scripts keyed on a $BLOBS dir. Verifies #8's
# generic command-based storage contract.
function write_custom_storage_config { # helper
  require_test_config

  local fixtures_dir="$BATS_TEST_TMPDIR/fixtures"
  mkdir -p "$fixtures_dir"

  local repo_root
  repo_root="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  cp "$repo_root/cmd/maneater/maneater.1" "$fixtures_dir/maneater.1"
  cp "$repo_root/cmd/maneater/maneater.toml.5" "$fixtures_dir/maneater.toml.5"

  export BLOBS="$BATS_TEST_TMPDIR/custom-store"

  local config_dir="$HOME/.config/maneater"
  mkdir -p "$config_dir"
  cat >"$config_dir/maneater.toml" <<EOF
[[corpora]]
name = "custom-docs"
type = "files"
paths = ["$fixtures_dir/*"]
max-chars = 500

[storage]
store-id = "custom-fs"
read-cmd   = ["sh", "-c", "cat \"\$BLOBS/\$1\"", "--"]
write-cmd  = ["sh", "-c", "t=\$(mktemp -p \"\$BLOBS\" .in.XXXXXX); cat >\"\$t\"; h=\$(sha256sum \"\$t\" | awk '{print \$1}'); mv \"\$t\" \"\$BLOBS/\$h\"; echo \$h"]
exists-cmd = ["sh", "-c", "test -d \"\$BLOBS\" && echo \"custom-fs: filesystem\""]
init-cmd   = ["sh", "-c", "mkdir -p \"\$BLOBS\""]
EOF
}

# Explicit `type = "manpages"` corpus alongside a files corpus (maneater#34),
# against the deterministic zz-fixtures/manpages tree. Guarded: maneater's
# manpath resolution always appends the system manpath (via manpath(1)), so
# unless manpath(1) honors $MANPATH and reports exactly the fixtures tree,
# the test would embed the host's entire man tree and blow the 120s timeout.
function manpages_corpus_indexes_alongside_files { # @test
  require_test_config

  # A 2-page subtree (one per section) of the deterministic fixtures:
  # the full 20-page tree costs ~3-4s/page (mandoc+pandoc+tldr+embed)
  # and blows run_maneater's 120s timeout.
  local repo_root man_tree
  repo_root="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  man_tree="$BATS_TEST_TMPDIR/man"
  mkdir -p "$man_tree/man1" "$man_tree/man5"
  cp "$repo_root/zz-fixtures/manpages/man1/alfabench.1" "$man_tree/man1/"
  cp "$repo_root/zz-fixtures/manpages/man5/alfaconf.5" "$man_tree/man5/"

  export MANPATH="$man_tree"
  if [[ "$(manpath 2>/dev/null)" != "$MANPATH" ]]; then
    # shellcheck disable=SC2016 # the skip reason names the variable, deliberately unexpanded
    skip 'manpath(1) does not honor $MANPATH exactly; host man tree would leak in'
  fi

  # Pre-create the tldr cache so prepare-cmd skips `tldr -u` (no network),
  # same mitigation as the bench-manpath justfile recipe.
  mkdir -p "$HOME/.cache/tldr/pages"

  local fixtures_dir="$BATS_TEST_TMPDIR/fixtures"
  mkdir -p "$fixtures_dir"
  echo "files corpus document" >"$fixtures_dir/doc.txt"

  local config_dir="$HOME/.config/maneater"
  mkdir -p "$config_dir"
  cat >"$config_dir/maneater.toml" <<EOF
[[corpora]]
type = "manpages"

[[corpora]]
name = "docs"
type = "files"
paths = ["$fixtures_dir/*"]
max-chars = 500
EOF

  init_maneater_store

  run_maneater index
  assert_success
  assert_output --partial "ok 1 - manpages/alfabench(1)"
  assert_output --partial "Done: manpages"
  assert_output --partial "Done: docs"
}

function custom_storage_init_creates_dir { # @test
  write_custom_storage_config

  run_maneater init-store
  assert_success
  [[ -d $BLOBS ]] || fail "init-cmd did not create $BLOBS"
}

function custom_storage_index_round_trips_via_commands { # @test
  write_custom_storage_config
  run_maneater init-store
  assert_success

  run_maneater index
  assert_success
  assert_output --partial "Done: custom-docs"

  # At least one blob file should now live under $BLOBS.
  local count
  count=$(find "$BLOBS" -type f | wc -l)
  [[ $count -ge 1 ]] || fail "expected >=1 blob in $BLOBS, got $count"

  # Re-running should use the post-hoc incremental path via read-cmd.
  run_maneater index
  assert_success
  assert_output --partial "from blob store"
}

function custom_storage_index_without_init_fails_fast { # @test
  write_custom_storage_config

  run_maneater index
  assert_failure
  assert_output --partial "init-store"
}
