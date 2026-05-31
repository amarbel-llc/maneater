# Index cache pruning

**Date:** 2026-05-31
**Status:** proposed
**Tracking:** [#2](https://github.com/amarbel-llc/maneater/issues/2) (content-addressed cache — pruning is the open tail of this issue)
**Related:** madder (no GC by design), nebulous [#11](https://github.com/amarbel-llc/nebulous/issues/11) ("GC becomes necessary")

## Problem

The content-addressed cache (issue #2) creates a new directory every time an
embedding-affecting config value changes:

```
$XDG_DATA_HOME/maneater/index/{corpusName}/{configHash}/
    manifest.json   ← { blobDigest, configHash }
    meta.json       ← debuggability snapshot
```

(`indexDataDir`, `commands/commands.go:21`.) Nothing ever removes the old
`{configHash}` directories. Switching a model, changing a prefix, bumping
n-ctx, or (per the companion web-corpus design) a chrest upgrade each strands
the previous directory plus its madder blob. Over a few experimentation cycles
a corpus accumulates a dozen dead directories.

This cannot be delegated downward. **madder has no garbage collection by
design** — it refuses to be a graph and has no reachability model. The CLI
maneater uses (`cat`/`write`/`list`/`init` via `internal/0/madder`) does not
even expose blob deletion; `DeleteBlobs`/`AllBlobs` exist only in madder's Go
library. So the live-set knowledge and the pruning policy must live in maneater.

maneater *does* hold the live set: for each corpus, the single current
`config.Hash` is live; every sibling `{configHash}` directory under that
corpus is dead.

## Design

### Two layers, two phases

| Layer | What's orphaned | Reclaimable today? |
|---|---|---|
| **Index data dirs** | `index/{corpus}/{oldHash}/` (manifest + meta) | **Yes** — maneater owns these files outright |
| **madder blobs** | The embedding blob a dead manifest pointed at | **No** — madder CLI has no delete |

**Phase 1 (this design): prune index data directories.** Pure local filesystem
work, fully in maneater's control, immediately useful — it stops the directory
sprawl and makes `index/{corpus}/` legible (one live dir + whatever the user
chose to keep).

**Phase 2 (blocked): reclaim orphaned blobs.** Requires a madder delete path on
the CLI surface maneater uses (or importing madder's Go `BlobStore` so
`DeleteBlobs` + `DeletionPrecondition` are reachable). Track upstream; do not
attempt until madder exposes it. Until then, a pruned directory leaves its blob
resident in the store — wasted disk, but not incorrect, and blobs are
content-addressed so a re-created identical config re-points at the surviving
blob for free.

### Live-set computation

For a prune of corpus `C`:

1. Resolve config exactly as `RunIndex` does (`config.LoadDefault`,
   `resolveCorpora`, per-corpus `ActiveModelForCorpus`), yielding the **current**
   `config.Hash` for `C` — the one live directory.
2. Enumerate `index/{C}/*` directories.
3. Every directory whose name ≠ the live hash is a prune candidate.

Pruning only ever removes maneater's own manifest/meta directories; it never
touches the shared blob store, so it is safe with respect to blobs another
corpus references (blobs are content-addressed and shared; the manifest is just
a pointer).

### Surface: explicit, never automatic

Pruning is **opt-in**, exposed as a subcommand:

```
maneater prune              # prune dead dirs for every configured corpus
maneater prune --corpus C   # scope to one corpus
maneater prune --dry-run    # list what would be removed, remove nothing
maneater prune --keep N     # keep the N most-recently-modified dead dirs (default 0)
```

Plus an optional convenience flag on index for the common case:

```
maneater index --prune      # index, then prune dead dirs for the indexed corpora
```

**Why not automatic after every index:** issue #2's rollback story is "revert
the code and old caches still work" — old-format and other-config directories
are deliberately disjoint and survivable. Auto-pruning on every index would
delete exactly the directories a reverted or alternately-configured binary
needs, breaking that guarantee. Keeping prune explicit preserves rollback and
makes the destructive step something the user asked for. `--keep N` gives a soft
landing for the "I might switch back to the previous model" case.

### Output

Prune emits the same TAP-14 stream the other subcommands use (via
`amarbel-llc/tap`), one line per directory:

```
TAP version 14
ok 1 - manpages/a1b2c3d4e5f6 # SKIP live
ok 2 - manpages/0011223344ff # removed (dead, blob blake2b256-… retained)
ok 3 - smart-docs/deadbeef00 # removed
1..3
```

The "blob … retained" note keeps the phase-2 gap visible: the user sees that
disk in the madder store is not yet reclaimed.

## Safety

- **Dry-run first-class.** `--dry-run` is the recommended first invocation and
  is cheap (stat only).
- **Never delete the live dir.** The live hash is recomputed from current
  config, not read from disk, so a corrupt `meta.json` cannot trick prune into
  deleting the wrong directory.
- **Never touch blobs in phase 1.** Removing a manifest is non-destructive to
  content; worst case re-index re-writes an identical blob (madder dedups it).
- **Unknown corpora.** A directory under `index/` whose corpus name is not in
  the current config (a corpus the user removed from TOML) is *not* auto-pruned
  by `maneater prune` without `--corpus` — report it as orphaned and let the
  user prune it explicitly, so dropping a corpus from config for one run doesn't
  silently delete its index.

## Phase 2 sketch (for when madder unblocks)

Once a madder delete path exists:

1. Before removing dir `D`, read its `manifest.json` blob digest.
2. Collect the digests still referenced by all *surviving* manifests (live +
   `--keep`).
3. For each digest referenced only by pruned dirs, call madder delete guarded by
   madder's `DeletionPrecondition` ("safe to delete from the loose store").

This is a per-corpus mark-and-sweep with maneater's manifests as the root set —
the reachability model madder declines to own, implemented where the edges
actually live. madder's FDR-0008 (config-digest pinning, digest-bearing
blob-store-ids) is the upstream work to watch; it may surface the delete
primitive this phase needs.
