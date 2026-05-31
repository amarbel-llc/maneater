# Web-corpus capture identity ↔ config hash join

**Date:** 2026-05-31
**Status:** proposed
**Tracking:** [#11](https://github.com/amarbel-llc/maneater/issues/11) (web corpus)
**Related:** [#2](https://github.com/amarbel-llc/maneater/issues/2) (content-addressed cache),
chrest [#53](https://github.com/amarbel-llc/chrest/issues/53) (capabilities artifact),
nebulous [#11](https://github.com/amarbel-llc/nebulous/issues/11) (versioned cache keys)

## Problem

maneater's incremental cache invalidates on two axes:

- **Config axis** — `config.Hash(model, corpus)` (`internal/0/config/confighash.go`)
  picks the cache *directory*. Folds in model path, document-prefix, n-ctx,
  pooling, max-chars, and the corpus's model selector.
- **Data axis** — each `Document.Hash` (SHA256 of source content) decides
  whether an individual entry is reused or re-embedded.

For file and manpage corpora the source bytes are stable and local, so both
axes work. A **web corpus** (issue #11: `type = "command"` shelling out to
chrest + madder) breaks an assumption baked into both axes: *the text we embed
is not a stable function of the URL.* The same URL re-rendered after a chrest
or browser upgrade can produce different extracted text — a different
`markdown-reader` body, a re-flowed `html-monolith`. Two failure modes follow:

1. **Silent staleness.** If the per-doc probe only hashes the URL, an
   extractor upgrade that changes every extracted body is invisible: the cache
   serves embeddings of the *old* extraction forever.
2. **Spurious churn.** If we naively content-hash the *captured bytes*, the
   non-text formats chrest emits (PDF, PNG, MHTML) are **not byte-deterministic**
   run to run — `/CreationDate`, `/ID`, PNG `tIME`, compression variance differ
   even when the page is unchanged (chrest #21, #22). Every warm index then
   re-embeds everything.

The fix is to make capture *identity* a first-class input to the cache key, and
to hash the *content-addressed id* rather than raw bytes.

## Upstream: what chrest already gives us

chrest's capture output is split three ways (Web Capture Archive Protocol,
chrest RFC 0001), and the split lines up exactly with maneater's two axes:

| chrest artifact | Contents | maneater axis |
|---|---|---|
| **payload** | The captured bytes, content-addressed to a markl-id (`blake2b256-…`) | data axis (`Document.Hash`) |
| **spec** | Resolved options + `capturer.capabilities_id` (JCS-canonical, deterministic) | config axis (`config.Hash`) |
| **envelope** | Per-run volatile context: timestamp, HTTP timing, headers | *neither* — must be excluded |

`capturer.capabilities_id` (chrest #53, schema `web-capture-archive.capabilities/v0`)
is a deterministic hash of `(chrest version, browser name+version, host platform)`
with **no time-varying fields**. It is precisely the "did the extraction
toolchain change" signal maneater needs, pre-computed upstream.

## Design

### The join

Fold the corpus's **capture identity** into the directory-level `config.Hash`,
and use chrest's **payload markl-id** (not raw bytes) as the document-level hash.

```
cache directory  =  index/{corpusName}/{configHash}/
configHash       =  Hash( model, corpus, captureIdentity )   ← new third input
Document.Hash    =  payload markl-id  (chrest content address, not raw bytes)
```

- A chrest/browser upgrade changes `captureIdentity` ⇒ new directory ⇒ full
  re-capture + re-embed of the web corpus. Correct, and isolated to that
  corpus (other corpora keep their directories).
- Between captures under one toolchain, per-URL content drift changes the
  payload markl-id ⇒ that one entry re-embeds. The data axis stays fine-grained.
- The envelope (timestamp, timing) never enters either hash, so a re-fetch that
  returns identical content is a true no-op.

### Where `captureIdentity` comes from (the runtime wrinkle)

`capabilities_id` is a property of the *installed chrest + browser at index
time*, not a static TOML value — so it cannot be computed at config-load the
way today's `config.Hash` inputs are. Two options:

**A. Probe at index/search time (recommended).** Extend the command-corpus
contract with an optional `identity-cmd` that prints a stable capture-identity
string (a chrest-backed web corpus wires it to `chrest capabilities` →
`capabilities_id`). `RunIndex`/`RunSearch` run it once per corpus during
`Prepare`, fold the result into the effective hash, and record it in
`meta.json`. Corpora without `identity-cmd` (files, manpages) pass the empty
string and behave exactly as today.

**B. Static TOML field.** A `capture-identity = "…"` key the user pins by hand.
Rejected: defeats the purpose — the whole point is that the toolchain version is
discovered, not remembered, and a stale hand-pinned value reintroduces silent
staleness.

`config.Hash` gains one trailing field; empty input must produce the **same
digest as today** so existing non-web caches round-trip (mirrors how
`ResolvedNCtx()` collapses 0 → 512 for backward compatibility):

```go
func Hash(model ModelConfig, corpus CorpusConfig, captureIdentity string) string {
	h := sha256.New()
	// ... existing fields, byte-for-byte unchanged ...
	if captureIdentity != "" {
		fmt.Fprintf(h, "capture-identity:%s\n", captureIdentity)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
```

### The cheap reuse probe (HashCmd fast path)

The command corpus already supports `HashCmd` (the synthesized manpages corpus
uses `maneater-man hash` — `commands.go:65`). For a web corpus, `HashCmd`
should return the **payload markl-id from the mapping layer without re-fetching**:

- A mapping layer (maneater's side of issue #11; nebulous #11 is the twin)
  stores `url → {payloadMarklId, capabilityId, capturedAt}`.
- `HashCmd <url>` looks up the last known `payloadMarklId` and prints it. If the
  hash matches `prev[url]`, the index loop reuses the cached entry and **never
  invokes `ReadCmd`** — no chrest fetch, no embed (`index.go:166-177`).
- `maneater ingest <corpus> <url>` (issue #11) is what actually (re-)captures:
  it drives chrest, writes the payload blob to madder, and updates the mapping
  layer's `payloadMarklId`. Routine `maneater index` is then a cheap reconcile
  against whatever `ingest` last recorded — it does not crawl.

This keeps the expensive operation (network + browser render) explicit and out
of the warm path, consistent with how the cache already separates "discover
what changed" from "embed what changed".

### Determinism contract

The data-axis hash MUST be the chrest payload markl-id, never the raw bytes:

- markl-id is computed by chrest over the (optionally normalized) artifact and
  is stable for stable content.
- For text formats (`markdown-*`, `text`, `html-*`) raw bytes happen to be
  deterministic too, so either would work — but
- For binary formats (PDF, PNG, MHTML) raw bytes are **not** deterministic, so
  hashing them would defeat incremental reuse entirely. Using the markl-id is
  the only correct choice across all formats.

This is a hard invariant for the web corpus; it should be asserted in tests
(capture the same fixture twice, assert equal markl-id, assert reuse).

## What does not change

- File / manpage / plain command corpora: `identity-cmd` absent ⇒ empty
  capture identity ⇒ identical `config.Hash` ⇒ identical cache directory. No
  rebuild, no migration.
- The blob store, manifest, and `meta.json` formats are unchanged except that
  `meta.json` gains a `captureIdentity` field for debuggability (additive).
- Search-time staleness handling is unchanged: `search.go` already warns and
  skips a corpus whose recorded `ConfigHash` no longer matches the recomputed
  one (`search.go:181-185`); folding capture identity in just makes that check
  also fire on a toolchain upgrade.

## Open questions

- **Where the mapping layer lives.** maneater-side package vs. delegated to the
  chrest/nebulous batch protocol's archive record. nebulous #11 wants the key
  scheme shared so entries are mutually interpretable; agreeing on
  `(url, capabilityId) → payloadMarklId` as the canonical shape is the
  coordination point.
- **Granularity of `captureIdentity`.** `capabilities_id` is global to a chrest
  install. If we ever want per-format identity (a PDF extractor upgrade
  shouldn't invalidate markdown captures), identity would move from the
  directory axis to the per-document hash. Out of scope until a real per-format
  divergence appears.
- **RFC 0002/0003 migration.** chrest #83 collapses the spec/envelope/payload
  triad into a merkle receipt. When that lands, `identity-cmd` reads the
  capability id out of the receipt tree instead of a flat `spec` — the join
  shape above is unaffected, only the extraction path changes.
