---
status: experimental
date: 2026-04-25
promotion-criteria: |
  Promote to `experimental` when a working `[[corpora]]` entry with
  `model = "qwen3-embedding-4b"` and `n-ctx = 4096` indexes a real
  small-doc corpus and survives a round-trip through `maneater search`
  without manual intervention.

  Promote to `testing` when the search-quality harness in
  `internal/0/embedding/search_quality_test.go` runs against both the
  current default profile and the smart profile, with measured
  per-query similarity deltas captured.

  Promote to `accepted` when the smart profile demonstrably beats the
  default on at least one focused real-world corpus the user cares
  about, and per-query latency stays within whatever budget the
  measurement reveals to be tolerable.
---

# Smart-retrieval corpus profile

## Problem Statement

Maneater today applies one global embedding model to every corpus, with a
hard-coded 512-token context budget. That's the right trade-off for
indexing thousands of man pages quickly, but it misses a different use
case: a tightly-scoped corpus of fewer than ~200 documents — one
project's docs, a single RFC set, a curated reading list — where the
absolute embed cost is low enough to afford a much larger, smarter
model and to embed each document whole instead of chunked. The current
config has no way to opt one corpus into that trade-off without
reconfiguring the whole tool.

## Interface

The smart profile is expressed as a combination of TOML schema
additions; nothing about the existing default behavior changes when
the new fields are absent.

### New `[models.X]` keys

| Key       | Type   | Default | Description                                                                                          |
|-----------|--------|---------|------------------------------------------------------------------------------------------------------|
| `n-ctx`   | int    | `512`   | Llama context size for this model. Larger values raise quality on long docs at memory + latency cost. |
| `pooling` | string | `""`    | One of `mean`, `cls`, `last`, or `""` (model default). Decoder-LLM encoders typically need `last`.    |

`query-prefix` and `document-prefix` already accept arbitrary strings,
including embedded newlines, so instruction-templated queries
(`"Instruct: ...\nQuery: "`) fit without further schema work.

### New `[[corpora]]` key

| Key     | Type   | Default        | Description                                                                                  |
|---------|--------|----------------|----------------------------------------------------------------------------------------------|
| `model` | string | `cfg.Default`  | Name of the `[models.X]` entry to use for this corpus's documents and the queries against it. |

A corpus's `max-chars = 0` means "no character truncation; the model's
`n-ctx` is the only clipping point." Existing corpora that omit
`max-chars` continue to behave as before.

### Confighash inputs

The cache key for each corpus folds in: model name, model path, model
`n-ctx`, model `pooling`, and `document-prefix`. Changing any of these
invalidates that corpus's cached entries without affecting other
corpora.

### First concrete profile: Qwen3-Embedding-4B at 4K context

Reference profile shipped alongside this feature:

```toml
[models.qwen3-embedding-4b]
path           = "/path/to/qwen3-embedding-4b.Q8_0.gguf"
n-ctx          = 4096
pooling        = "last"
query-prefix   = "Instruct: Given a search query, retrieve relevant passages that answer the query.\nQuery: "
document-prefix = ""
```

The instruction string is part of the prefix, not a separate config
field — this keeps the schema flat and lets users swap in different
task templates per corpus by defining additional `[models.X]` entries.

## Examples

### Two profiles in one config

```toml
default = "snowflake"

[models.snowflake]
path           = "/nix/store/.../snowflake-arctic-embed-l-v2.0-q8_0.gguf"
n-ctx          = 512
query-prefix   = "query: "
document-prefix = ""

[models.qwen3-embedding-4b]
path           = "/nix/store/.../qwen3-embedding-4b.Q8_0.gguf"
n-ctx          = 4096
pooling        = "last"
query-prefix   = "Instruct: Given a search query, retrieve relevant passages that answer the query.\nQuery: "
document-prefix = ""

# Manpages corpus uses the default (snowflake) model.
# Synthesized automatically when no [[corpora]] entries exist;
# shown here for clarity.

[[corpora]]
name      = "project-docs"
type      = "files"
paths     = ["docs/**/*.md", "docs/**/*.rst"]
max-chars = 0                       # no truncation; let n-ctx clip
model     = "qwen3-embedding-4b"    # opt this corpus into the smart profile
```

`maneater index` on this config:

1. Loads `snowflake` and indexes the manpages corpus at 512 ctx.
2. Loads `qwen3-embedding-4b` and indexes the project-docs corpus at
   4096 ctx, running each whole-document body through the instruct
   prefix.

`maneater search "how does the cache invalidate"` re-embeds the query
twice — once per active model — and merges the per-corpus result
sets. Models are loaded lazily on first need within a process.

### Single smart-profile-only config

```toml
default = "qwen3-embedding-4b"

[models.qwen3-embedding-4b]
path  = "/path/to/qwen3-embedding-4b.Q8_0.gguf"
n-ctx = 4096
pooling = "last"
query-prefix    = "Instruct: Given a search query, retrieve relevant passages that answer the query.\nQuery: "
document-prefix = ""

[[corpora]]
name  = "rfcs"
type  = "files"
paths = ["~/refs/rfcs/*.txt"]
# model omitted ⇒ uses cfg.Default
```

## Limitations

- **Memory cost.** A 4B-parameter model at Q8_0 is ~4 GB on disk and
  in memory. The KV cache for 4K context adds further memory per
  active embed. The smart profile is opt-in for this reason.
- **Per-query latency.** Every `maneater search` re-embeds the query
  through the corpus's chosen model. With Qwen3-4B on CPU this is
  measured in seconds, not milliseconds. Tolerable for an interactive
  CLI invocation, problematic for any future "live search as you
  type" mode.
- **Multi-model sessions load every active model.** If two corpora in
  one config use two different models, a single `maneater search`
  pays both load costs (mitigated by lazy loading and OS file cache,
  but not zero).
- **No auto-fetch of GGUFs.** Models are referenced by absolute path.
  Distribution is the user's problem (or the nix flake's, for shipped
  configs).
- **GGUF availability is the gating constraint.** A model named in
  `[models.X]` is only usable if a GGUF file exists for it that
  llama.cpp's embedding mode can load. This is a much narrower set
  than "models with HF safetensors" — see the survey of HF GGUF
  embedding models that informed this design.
- **Instruction templating lives in `query-prefix`, not a structured
  task field.** This is deliberate (keeps the schema flat) but means
  per-task templating across one corpus requires defining a separate
  `[models.X]` per task variant.
- **Pooling type is a model-level knob, not a corpus-level knob.**
  Two corpora using the same `[models.X]` entry necessarily share
  pooling. If a use case for per-corpus pooling emerges, the right
  fix is to define a second `[models.X]` with the same path and a
  different pooling value.

## Memory tuning

The 4 GB Q8_0 footprint dominates per-search peak RSS. Two cheap
levers reduce it without a quality cliff:

### 1. Lower quantization on the same model

Same Qwen3-Embedding-4B, smaller GGUF:

| Quant   | Approx size | Δ vs Q8_0 |
|---------|-------------|-----------|
| Q8_0    | ~4.0 GB     | baseline  |
| Q6_K    | ~3.3 GB     | -18 %     |
| Q5_K_M  | ~2.9 GB     | -28 %     |
| Q4_K_M  | ~2.5 GB     | -38 %     |

All five are present in the official `Qwen/Qwen3-Embedding-4B-GGUF`
repo. Switching profile = a `fetchGgufModel` URL change in
`flake.nix` plus a fresh `gguf-sri-hash` lookup. Quality cost is
measurable via `search_quality_test.go` once that runs against the
smart profile (the FDR's `experimental → testing` gate). Q4_K_M is
the conventional Pareto choice for retrieval; for the small-corpus
use case Q8_0's marginal benefit may not be worth the +1.5 GB.

### 2. KV-cache quantization

Independent of the model footprint: llama.cpp's context params
expose `type_k` and `type_v`, defaulting to f16. Setting both to
`GGML_TYPE_Q8_0` halves the per-call KV-cache memory. At
n-ctx=4096 with the qwen3-4b head dim, this is meaningful but
much smaller than the model itself.

Schema shape if/when wired up: a new `[models.X].kv-quant` field
accepting `""` (default = f16), `"q8_0"`, `"q4_0"`, or similar.
Threads through `NewEmbedder` to `cp.type_k = …; cp.type_v = …`
and folds into `confighash.Hash` so the cache invalidates if a
config changes the precision.

### Out-of-scope here

- **Model variant swap** (e.g. Qwen3-Embedding-0.6B, ~700 MB) —
  not memory tuning of the shipped profile, it's a different
  `[models.X]` entry. The FDR's per-corpus selection already
  supports this; ship multiple stanzas and let users pick.
- **`use_mlock`** would *raise* steady-state memory by pinning
  pages, the wrong direction here.
- **Daemon process** would amortize the 4 GB across many searches
  but increase resident memory globally, again the wrong direction.

## Implementation status (experimental, 2026-04-25)

The schema additions, embedder plumbing, and reference profile are
landed. End-to-end smoke against this repo's `docs/**/*.md` corpus
with Qwen3-Embedding-4B Q8_0 at n-ctx=4096 + pooling=last:

| Step | Wall-clock | Outcome |
|---|---|---|
| `init-store` | 0.05 s | ok |
| `index --force` (cold) | **142 s** | 5/5 docs embedded, no failures |
| `index` (warm) | **5.4 s** | 5/5 reused — true no-op |
| `search "how does the cache invalidate"` | 8.1 s | #1 cache-design.md (0.46), #2 cache-plan.md (0.35) |
| `search "feature design record"` | 7.9 s | #1 0001-smart-retrieval-corpus-profile.md (0.21) |
| `search "embedding model context length"` | 7.7 s | #1 0001-smart-retrieval-corpus-profile.md (0.52) |

Cold index ≈ 28 s/doc on CPU at 4K context; per-search latency is
dominated by model load (~7 s of each ~8 s figure). All three queries
put the expected doc at #1.

Reproduce with `just smart-profile-smoke`.

## More Information

- The llama.cpp embedder lives in `internal/0/embedding/llama.go`. The
  encode-vs-decode dispatch and KV-cache reset between embeds are
  modeled as a `batchStrategy` interface with `encoderStrategy` and
  `decoderStrategy` impls — selected at `NewEmbedder` time via
  `llama_model_has_encoder(model)`.
- `ActiveModelForCorpus(cfg, corpus)` in `internal/0/config/config.go`
  is the per-corpus resolver. `ActiveModel(cfg)` remains for callers
  that don't have corpus context.
- `internal/0/config/confighash.go` folds in `model.Path`,
  `model.DocumentPrefix`, `model.NCtx` (resolved), `model.Pooling`,
  `corpus.MaxChars`, and `corpus.Model` so cache invalidates on any
  embedding-affecting change.
- Open follow-ups discovered during the smoke test: maneater#22
  (failed embeds re-attempted on every warm pass instead of memoized)
  and maneater#23 (failed embeds counted toward the success tally).
  Neither blocks `experimental` but both should land before
  `accepted`.
