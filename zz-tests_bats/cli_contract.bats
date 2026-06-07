#!/usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=cli

function bare_invocation_prints_usage { # @test
  run_maneater
  assert_success
  assert_output --partial "maneater"
  assert_output --partial "Commands"
}

function search_without_query_fails { # @test
  run_maneater search
  assert_failure
  assert_output --partial "usage"
}

# A `type = "manpages"` corpus synthesizes its own maneater-man commands;
# user-supplied command fields are rejected (maneater#34). The rejection
# happens during config resolution, before any store or model init, so
# this needs neither MANEATER_TEST_CONFIG nor an initialized store.
function manpages_corpus_rejects_command_fields { # @test
  local config_dir="$HOME/.config/maneater"
  mkdir -p "$config_dir"
  cat >"$config_dir/maneater.toml" <<'EOF'
[[corpora]]
type = "manpages"
list-cmd = ["ls"]
EOF

  run_maneater index
  assert_failure
  assert_output --partial 'type "manpages" does not accept list-cmd'
}
