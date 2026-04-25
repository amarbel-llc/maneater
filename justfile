# Build, test, and check
default: build test test-bats check-dagnabit

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

# Dry-run dagnabit reposition to see how the internal/ DAG has drifted from NATO tiering.
[group('check')]
check-dagnabit:
  dagnabit -n -v internal

# Apply dagnabit reposition to realign internal/ packages with NATO tiering.
[group('dev')]
codemod-dagnabit:
  dagnabit -v internal

# Look up the SRI sha256 of a HuggingFace LFS file (e.g. a GGUF model
# weight) without downloading the file. Reads the git-LFS OID via the
# HF tree API and converts it to nix SRI format. Useful when adding or
# updating a fetchGgufModel entry in flake.nix.
#
# Usage:
#   just gguf-sri-hash https://huggingface.co/Qwen/Qwen3-Embedding-4B-GGUF/resolve/main/Qwen3-Embedding-4B-Q8_0.gguf
[group('dev')]
gguf-sri-hash url:
  #!/usr/bin/env bash
  set -euo pipefail
  url="{{url}}"
  if [[ ! "$url" =~ ^https://huggingface\.co/([^/]+)/([^/]+)/resolve/([^/]+)/(.+)$ ]]; then
    echo "URL must look like https://huggingface.co/<owner>/<repo>/resolve/<branch>/<filename>" >&2
    exit 1
  fi
  owner="${BASH_REMATCH[1]}"
  repo="${BASH_REMATCH[2]}"
  branch="${BASH_REMATCH[3]}"
  filename="${BASH_REMATCH[4]}"
  api="https://huggingface.co/api/models/${owner}/${repo}/tree/${branch}"
  hex=$(curl -sSL "$api" | jq -r --arg fn "$filename" '.[] | select(.path == $fn) | .lfs.oid // empty')
  if [[ -z "$hex" ]]; then
    echo "no LFS OID found for $filename in $api (file may not be LFS-stored)" >&2
    exit 1
  fi
  nix hash convert --to sri --hash-algo sha256 "$hex"

# Snapshot HF's GGUF + feature-extraction model list for offline
# analysis. Reusable for "what's currently shipping as GGUF embedding"
# surveys; output lands at .tmp/hf-gguf-embed.json by default.
# Smoke-test the FDR-0001 smart-retrieval corpus profile end-to-end:
# index this repo's docs/ tree with Qwen3-Embedding-4B at 4K context
# and run a couple of search queries against it. Times each step so
# the cold-start and per-query latency footprints are visible.
#
# First run downloads the Qwen3 Q8_0 GGUF (~4 GB) via nix and warms
# the model into RAM. Subsequent runs hit the page cache. Indexing
# ~5 markdown docs in this repo is the entire corpus on purpose:
# the FDR's framing is small, focused corpora.
#
# See docs/features/0001-smart-retrieval-corpus-profile.md.
[group('explore')]
smart-profile-smoke: build-wrapped
  #!/usr/bin/env bash
  set -euo pipefail

  ROOT="$(mktemp -d /tmp/maneater-smart-smoke.XXXXXX)"
  trap 'rm -rf "$ROOT"' EXIT

  HOME_DIR="$ROOT/home"
  mkdir -p "$HOME_DIR/.config/maneater" "$HOME_DIR/.local/share" "$HOME_DIR/.cache"

  # Project-local maneater.toml flips the default to qwen3-embedding-4b
  # and points one corpus at this repo's docs/ tree. The base wrapped
  # config (which defines [models.qwen3-embedding-4b] and
  # [models.snowflake]) is inherited via $MANEATER_CONFIG.
  # Project-local config inherits [models.*] from the base wrapped
  # config (n-ctx + pooling already set there). We only need to flip
  # the default and declare the corpus.
  cat >"$HOME_DIR/maneater.toml" <<EOF
  default = "qwen3-embedding-4b"

  [[corpora]]
  name = "smart-docs"
  type = "files"
  paths = ["{{justfile_directory()}}/docs/**/*.md"]
  max-chars = 0
  model = "qwen3-embedding-4b"
  EOF

  MANEATER="{{justfile_directory()}}/build/result-wrapped/bin/maneater"
  export HOME="$HOME_DIR"
  export XDG_DATA_HOME="$HOME_DIR/.local/share"
  export XDG_CACHE_HOME="$HOME_DIR/.cache"
  export XDG_CONFIG_HOME="$HOME_DIR/.config"
  export MADDER_CEILING_DIRECTORIES="$ROOT"
  # Intentionally leave MANEATER_CONFIG unset so the wrapper supplies
  # the BASE config (which defines both [models.snowflake] and
  # [models.qwen3-embedding-4b]). MANEATER_TEST_CONFIG only has
  # snowflake, so it would break the qwen3 reference.

  cd "$HOME_DIR"

  echo "==> init-store"
  /usr/bin/time -f '  %e seconds' "$MANEATER" init-store

  echo
  echo "==> index (cold; loads model + embeds every doc)"
  /usr/bin/time -f '  %e seconds' "$MANEATER" index --force

  echo
  echo "==> index (warm; should be a no-op via incremental cache)"
  /usr/bin/time -f '  %e seconds' "$MANEATER" index

  echo
  echo "==> search 'how does the cache invalidate' --top-k 3"
  /usr/bin/time -f '  %e seconds' "$MANEATER" search "how does the cache invalidate" --top-k 3 || true

  echo
  echo "==> search 'feature design record' --top-k 3"
  /usr/bin/time -f '  %e seconds' "$MANEATER" search "feature design record" --top-k 3 || true

  echo
  echo "==> search 'embedding model context length' --top-k 3"
  /usr/bin/time -f '  %e seconds' "$MANEATER" search "embedding model context length" --top-k 3 || true

[group('explore')]
hf-gguf-embed url='https://huggingface.co/api/models?filter=gguf,feature-extraction&full=false&limit=1000&sort=downloads&direction=-1' out='.tmp/hf-gguf-embed.json':
  curl -sSL "{{url}}" -o "{{out}}"
  echo "Saved $(wc -c < {{out}}) bytes to {{out}}"
  echo "Records: $(jq 'length' {{out}})"

# Companion to hf-gguf-embed: fetches results 1001-2000 (the API caps
# limit=1000, so we paginate via skip).
[group('explore')]
hf-gguf-embed-page2:
  curl -sSL 'https://huggingface.co/api/models?filter=gguf,feature-extraction&full=false&limit=1000&sort=downloads&direction=-1&skip=1000' -o .tmp/hf-gguf-embed-page2.json
  echo "Records: $(jq 'length' .tmp/hf-gguf-embed-page2.json)"

# Cross-check using `?other=` instead of `?filter=`. HF's two filter
# modes return different populations; comparing them surfaces tag
# mismatches.
[group('explore')]
hf-gguf-embed-other:
  curl -sSL 'https://huggingface.co/api/models?other=gguf,feature-extraction&full=false&limit=1000&sort=downloads&direction=-1' -o .tmp/hf-gguf-embed-other.json
  echo "Records: $(jq 'length' .tmp/hf-gguf-embed-other.json)"

# Just the headers — useful for confirming pagination cursors and
# rate-limit info before pulling the body.
[group('explore')]
hf-gguf-embed-headers:
  curl -sSLI 'https://huggingface.co/api/models?filter=gguf,feature-extraction&full=false&limit=1000&sort=downloads&direction=-1'

# Concatenate page1 + page2 snapshots into a single JSON array for jq
# aggregations across the merged set.
[group('explore')]
hf-gguf-embed-merge:
  jq -s 'add' .tmp/hf-gguf-embed.json .tmp/hf-gguf-embed-page2.json > .tmp/hf-gguf-embed-all.json
  echo "Total: $(jq 'length' .tmp/hf-gguf-embed-all.json)"

[group('explore')]
man-tree:
  mkdir -p build/man/man1 build/man/man5
  ln -sf ../../../cmd/maneater/maneater.1 build/man/man1/maneater.1
  ln -sf ../../../cmd/maneater/maneater.toml.5 build/man/man5/maneater.toml.5

# Run bats integration tests (against the wrapped binary so madder is on its PATH).
# --no-sandbox opts out of batman's sandcastle wrapper so the wrapped maneater
# can reach the Metal GPU on darwin for real embedding inference; sandcastle
# has no metal/gpu passthru yet (see amarbel-llc/bob#106).
[group('test')]
test-bats: build-wrapped
  MANEATER_BIN={{justfile_directory()}}/build/result-wrapped/bin/maneater bats --no-sandbox zz-tests_bats/

# Format code
fmt:
  gofumpt -w .
  goimports -w .

# Regenerate the deterministic bench-manpath fixture pages
# (zz-fixtures/manpages/man{1,5}/*). Output is byte-stable.
[group('dev')]
gen-manpages-fixtures:
  zz-fixtures/manpages/gen.sh

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
