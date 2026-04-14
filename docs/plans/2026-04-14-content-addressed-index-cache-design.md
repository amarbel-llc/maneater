# Content-Addressed Index Cache

**Date:** 2026-04-14
**Issue:** [#2](https://github.com/amarbel-llc/maneater/issues/2)
**Status:** approved

## Problem

The index cache is keyed by `{corpusName}/{modelName}`. Changing config
parameters that affect embeddings (DocumentPrefix, model file, max-chars) serves
stale results silently. Additionally, the cache is all-or-nothing — adding or
removing a single man page requires a full rebuild.

## Design

### Two-Layer Cache Structure

**Directory layer** — keyed by corpus name and a config hash derived from
parameters that affect all embeddings globally:

```
$XDG_CACHE_HOME/maneater/index/{corpusName}/{configHash}/
    entries.jsonl
    meta.json
```

`configHash` is the first 12 hex characters of SHA256 over:

- Model file path (on NixOS, the store path changes when the model changes)
- `DocumentPrefix`
- Corpus-specific parameters (e.g. `max-chars` for files corpus)

Changing any of these creates a new directory and triggers a full rebuild.

**Entry layer** — within a directory, each entry tracks a content hash of its
source document. On `maneater index`, entries with matching hashes are reused;
only new or changed documents are embedded.

### Entry Format

Single `entries.jsonl` file, one JSON object per line:

```json
{"key": "ls(1)", "hash": "e3b0c44298fc...", "embedding": [0.123, -0.456, ...]}
```

- `key` — unique document identifier
- `hash` — hex SHA256 of the document's source content
- `embedding` — float32 vector

> **Future consideration:** this format may need to become binary for
> performance as corpus sizes grow. Additionally, a flat embedding lake
> (per-entry config rather than per-directory) could allow sharing embeddings
> across corpus configurations.

### Metadata Sidecar

`meta.json` stores the full config snapshot for debuggability:

```json
{
  "modelPath": "/nix/store/...-nomic.gguf",
  "documentPrefix": "search_document: ",
  "configHash": "a1b2c3d4e5f6"
}
```

### Content Hashing by Corpus Type

| Corpus type | What gets hashed |
|-------------|-----------------|
| manpages | Raw roff source file (avoids running mandoc+pandoc pipeline) |
| files | Raw file content |
| command | `read-cmd` output for the key (must run read-cmd every index to check) |

### Incremental Index Flow (`maneater index`)

1. Resolve active model config, compute `configHash`, derive cache directory.
2. If `entries.jsonl` exists, load into `map[key] -> {hash, embedding}`.
3. For each corpus:
   a. Enumerate current documents via `corpus.Documents()`.
   b. For each document, compute content hash from source.
   c. If existing entry has the same key and hash, reuse its embedding.
   d. Otherwise, embed the document.
4. Write `entries.jsonl` with all current entries (reused + newly embedded).
   Documents present in the old index but absent from the current enumeration
   are dropped (implicit removal).
5. Write/update `meta.json`.

`maneater index --force` skips step 2 (treats existing as empty), forcing a full
rebuild. This handles edge cases like mandoc/pandoc version changes affecting
extraction output.

### Search Behavior

`maneater search` is unchanged — it loads `entries.jsonl`, builds the in-memory
index, and searches. No staleness detection, no auto-rebuild.

### Backward Compatibility

Old format (`pages.txt` + `embeddings.jsonl` under `{corpusName}/{modelName}/`)
and new format live in separate directories — no collision. Old indexes are
ignored, not migrated. Rolling back is a code revert; old cache directories
remain usable by old code.
