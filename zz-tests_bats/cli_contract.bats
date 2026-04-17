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
