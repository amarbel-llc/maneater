{ pkgs, snowflake-model, qwen3-embedding-4b-model }:
let
  snowflakeStanza = ''
    [models.snowflake]
    path = "${snowflake-model}"
    query-prefix = "query: "
    document-prefix = ""
  '';

  # Smart-retrieval reference profile. See
  # docs/features/0001-smart-retrieval-corpus-profile.md.
  # n-ctx capped at 4096 (per FDR; the model itself supports 32K) and
  # pooling = "last" because Qwen3-Embedding is decoder-LLM-derived.
  qwen3EmbeddingStanza = ''
    [models.qwen3-embedding-4b]
    path = "${qwen3-embedding-4b-model}"
    query-prefix = "Instruct: Given a search query, retrieve relevant passages that answer the query.\nQuery: "
    document-prefix = ""
    n-ctx = 4096
    pooling = "last"
  '';
in
{
  test = pkgs.writeText "maneater-test.toml" snowflakeStanza;
  base = pkgs.writeText "maneater.toml" ''
    default = "snowflake"

    ${snowflakeStanza}
    ${qwen3EmbeddingStanza}
    # No [[corpora]] entries: maneater's synthesized default
    # activates a `type = "command"` manpages corpus that shells
    # out to maneater-man. See internal/charlie/commands.defaultManpagesCorpusConfig.
    # The qwen3-embedding-4b model is available but only used by
    # corpora that explicitly opt in via `model = "qwen3-embedding-4b"`.
  '';
}
