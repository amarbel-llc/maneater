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

# Run wall-clock bench against a synthetic 200-file type=command corpus.
# Uses the wrapped binary (so madder is on PATH) and MANEATER_TEST_CONFIG
# (from the nix devshell). Results appended to docs/bench/<date>-bench.md.
[group('bench')]
bench: build-wrapped
  #!/usr/bin/env bash
  set -euo pipefail

  : "${MANEATER_TEST_CONFIG:?run inside nix devshell (direnv)}"
  command -v strace >/dev/null || { echo "strace not on PATH" >&2; exit 1; }

  BENCH_ROOT="$(mktemp -d /tmp/maneater-bench.XXXXXX)"
  trap 'rm -rf "$BENCH_ROOT"' EXIT

  GITSHA=$(git -C "{{justfile_directory()}}" rev-parse --short HEAD)

  DOCS=200
  CORPUS_DIR="$BENCH_ROOT/corpus"
  mkdir -p "$CORPUS_DIR"
  for i in $(seq 1 "$DOCS"); do
    printf 'Document %d\nSynthetic payload for embedding benchmark.\nKey %d has a deterministic text body.\n' "$i" "$i" >"$CORPUS_DIR/doc-$i.txt"
  done

  HOME_DIR="$BENCH_ROOT/home"
  mkdir -p "$HOME_DIR/.config/maneater" "$HOME_DIR/.local/share" "$HOME_DIR/.cache"

  cat >"$HOME_DIR/.config/maneater/maneater.toml" <<EOF
  [[corpora]]
  name = "bench"
  type = "command"
  list-cmd = ["sh", "-c", "ls '$CORPUS_DIR' | grep '\\\\.txt\$'"]
  read-cmd = ["sh", "-c", "cat '$CORPUS_DIR'/\"\$1\"", "--"]
  max-chars = 500
  EOF

  MANEATER="{{justfile_directory()}}/build/result-wrapped/bin/maneater"
  export HOME="$HOME_DIR"
  export XDG_DATA_HOME="$HOME_DIR/.local/share"
  export XDG_CACHE_HOME="$HOME_DIR/.cache"
  export XDG_CONFIG_HOME="$HOME_DIR/.config"
  export MADDER_CEILING_DIRECTORIES="$BENCH_ROOT"
  export MANEATER_CONFIG="$MANEATER_TEST_CONFIG"

  # cd inside the ceiling so dewey's walk-from-cwd doesn't hit host ~/.madder.
  cd "$BENCH_ROOT"

  "$MANEATER" init-store >/dev/null

  time_run() {
    local log; log="$(mktemp)"
    /usr/bin/time -f '%e' -o "$log" "$@" >/dev/null 2>&1
    cat "$log"
    rm -f "$log"
  }

  median() {
    sort -g | awk '{a[NR]=$1} END { n=NR; if(n%2){print a[(n+1)/2]} else {printf "%.3f\n",(a[n/2]+a[n/2+1])/2} }'
  }

  # warm-up (first run pays model-load, fs-cache costs)
  "$MANEATER" index --force >/dev/null

  echo "Running full-rebuild trials..." >&2
  FULL_TIMES=()
  for trial in 1 2 3; do
    T=$(time_run "$MANEATER" index --force)
    FULL_TIMES+=("$T")
    echo "  trial $trial: ${T}s" >&2
  done
  FULL_MED=$(printf '%s\n' "${FULL_TIMES[@]}" | median)

  echo "Running incremental trials..." >&2
  INC_TIMES=()
  for trial in 1 2 3; do
    T=$(time_run "$MANEATER" index)
    INC_TIMES+=("$T")
    echo "  trial $trial: ${T}s" >&2
  done
  INC_MED=$(printf '%s\n' "${INC_TIMES[@]}" | median)

  echo "Collecting strace summaries..." >&2
  STRACE_FULL="$BENCH_ROOT/strace-full.log"
  STRACE_INC="$BENCH_ROOT/strace-inc.log"
  strace -c -f -o "$STRACE_FULL" "$MANEATER" index --force >/dev/null 2>&1 || true
  strace -c -f -o "$STRACE_INC" "$MANEATER" index >/dev/null 2>&1 || true

  TODAY=$(date +%Y-%m-%d)
  OUT="{{justfile_directory()}}/docs/bench/$TODAY-bench.md"
  mkdir -p "$(dirname "$OUT")"
  {
    echo "## Run at $(date -u +%H:%M:%SZ) on $GITSHA"
    echo
    echo "- Corpus: $DOCS synthetic text files, type = command"
    echo "- list-cmd: \`sh -c 'ls \$CORPUS_DIR | grep .txt\$'\`"
    echo "- read-cmd: \`sh -c 'cat \$CORPUS_DIR/\$1' --\`"
    echo "- Model: snowflake (from MANEATER_TEST_CONFIG)"
    echo "- 3 trials each, median wall-clock; warm-up run discarded"
    echo
    echo "| Metric | Median (s) | Trials |"
    echo "|---|---|---|"
    echo "| Full rebuild (\`index --force\`) | $FULL_MED | ${FULL_TIMES[*]} |"
    echo "| Incremental (\`index\`) | $INC_MED | ${INC_TIMES[*]} |"
    echo
    echo "### strace -c (full rebuild, one run)"
    echo
    echo '```'
    cat "$STRACE_FULL"
    echo '```'
    echo
    echo "### strace -c (incremental, one run)"
    echo
    echo '```'
    cat "$STRACE_INC"
    echo '```'
    echo
  } >>"$OUT"

  echo
  echo "Full rebuild median: ${FULL_MED}s"
  echo "Incremental median:  ${INC_MED}s"
  echo "Results appended to $OUT"

# Run wall-clock bench against the default (manpages) corpus using the
# system manpath. Captures the expensive-read-cmd case (mandoc + pandoc
# + tldr per page) that the synthetic `bench` recipe does not exercise.
# Results appended to docs/bench/<date>-bench.md.
[group('bench')]
bench-manpath: build-wrapped
  #!/usr/bin/env bash
  set -euo pipefail

  : "${MANEATER_TEST_CONFIG:?run inside nix devshell (direnv)}"
  command -v strace >/dev/null || { echo "strace not on PATH" >&2; exit 1; }

  BENCH_ROOT="$(mktemp -d /tmp/maneater-bench-manpath.XXXXXX)"
  trap 'rm -rf "$BENCH_ROOT"' EXIT

  GITSHA=$(git -C "{{justfile_directory()}}" rev-parse --short HEAD)

  HOME_DIR="$BENCH_ROOT/home"
  mkdir -p "$HOME_DIR/.config/maneater" "$HOME_DIR/.local/share" "$HOME_DIR/.cache"

  # Pre-create an empty tldr cache so EnsureTldrCache skips `tldr -u` (no
  # network) and ExtractTldr returns "" for every fixture page. Makes the
  # bench deterministic and fully offline.
  mkdir -p "$HOME_DIR/.cache/tldr/pages/osx" "$HOME_DIR/.cache/tldr/pages/common"

  # Empty user config — the default (manpages) corpus activates.
  : >"$HOME_DIR/.config/maneater/maneater.toml"

  MANEATER="{{justfile_directory()}}/build/result-wrapped/bin/maneater"
  export HOME="$HOME_DIR"
  export XDG_DATA_HOME="$HOME_DIR/.local/share"
  export XDG_CACHE_HOME="$HOME_DIR/.cache"
  export XDG_CONFIG_HOME="$HOME_DIR/.config"
  export MADDER_CEILING_DIRECTORIES="$BENCH_ROOT"
  export MANEATER_CONFIG="$MANEATER_TEST_CONFIG"

  # Strict, deterministic manpath: versioned fixtures only. 15 man1 + 5 man5
  # roff pages crafted to exercise mandoc + pandoc + tldr per page without
  # pulling in whatever the host system happens to have installed.
  export MANPATH="{{justfile_directory()}}/zz-fixtures/manpages"
  cd "$BENCH_ROOT"

  "$MANEATER" init-store >/dev/null

  time_run() {
    local log; log="$(mktemp)"
    /usr/bin/time -f '%e' -o "$log" "$@" >/dev/null 2>&1
    cat "$log"
    rm -f "$log"
  }

  median() {
    sort -g | awk '{a[NR]=$1} END { n=NR; if(n%2){print a[(n+1)/2]} else {printf "%.3f\n",(a[n/2]+a[n/2+1])/2} }'
  }

  # warm-up (first run pays model-load + fs-cache + tldr cache build).
  WARMUP_LOG="$BENCH_ROOT/warmup.log"
  "$MANEATER" index --force >"$WARMUP_LOG" 2>&1 || true
  PAGE_COUNT=$(grep -oE 'Done: manpages — [0-9]+' "$WARMUP_LOG" | awk '{print $4}' || echo "?")

  TRIALS="${BENCH_TRIALS:-1}"

  echo "Running full-rebuild trials (${PAGE_COUNT} pages, ${TRIALS} trial(s))..." >&2
  FULL_TIMES=()
  for trial in $(seq 1 "$TRIALS"); do
    T=$(time_run "$MANEATER" index --force)
    FULL_TIMES+=("$T")
    echo "  trial $trial: ${T}s" >&2
  done
  FULL_MED=$(printf '%s\n' "${FULL_TIMES[@]}" | median)

  echo "Running incremental trials..." >&2
  INC_TIMES=()
  for trial in $(seq 1 "$TRIALS"); do
    T=$(time_run "$MANEATER" index)
    INC_TIMES+=("$T")
    echo "  trial $trial: ${T}s" >&2
  done
  INC_MED=$(printf '%s\n' "${INC_TIMES[@]}" | median)

  STRACE_FULL="$BENCH_ROOT/strace-full.log"
  STRACE_INC="$BENCH_ROOT/strace-inc.log"
  if [[ "${BENCH_STRACE:-0}" == "1" ]]; then
    echo "Collecting strace summaries..." >&2
    strace -c -f -o "$STRACE_FULL" "$MANEATER" index --force >/dev/null 2>&1 || true
    strace -c -f -o "$STRACE_INC" "$MANEATER" index >/dev/null 2>&1 || true
  else
    echo "skipped (set BENCH_STRACE=1 to collect)" >"$STRACE_FULL"
    echo "skipped (set BENCH_STRACE=1 to collect)" >"$STRACE_INC"
  fi

  TODAY=$(date +%Y-%m-%d)
  OUT="{{justfile_directory()}}/docs/bench/$TODAY-bench.md"
  mkdir -p "$(dirname "$OUT")"
  {
    echo "## Run at $(date -u +%H:%M:%SZ) on $GITSHA (manpath corpus)"
    echo
    echo "- Corpus: default manpages corpus, $PAGE_COUNT pages from system manpath"
    echo "- Model: snowflake (from MANEATER_TEST_CONFIG)"
    echo "- 3 trials each, median wall-clock; warm-up (also builds tldr cache) discarded"
    echo
    echo "| Metric | Median (s) | Trials |"
    echo "|---|---|---|"
    echo "| Full rebuild (\`index --force\`) | $FULL_MED | ${FULL_TIMES[*]} |"
    echo "| Incremental (\`index\`) | $INC_MED | ${INC_TIMES[*]} |"
    echo
    echo "### strace -c (full rebuild, one run)"
    echo
    echo '```'
    cat "$STRACE_FULL"
    echo '```'
    echo
    echo "### strace -c (incremental, one run)"
    echo
    echo '```'
    cat "$STRACE_INC"
    echo '```'
    echo
  } >>"$OUT"

  echo
  echo "Full rebuild median: ${FULL_MED}s ($PAGE_COUNT pages)"
  echo "Incremental median:  ${INC_MED}s"
  echo "Results appended to $OUT"
