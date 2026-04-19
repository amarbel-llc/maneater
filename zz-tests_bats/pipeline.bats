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
