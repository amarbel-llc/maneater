{ pkgs, snowflake-model }:
let
  snowflakeStanza = ''
    [models.snowflake]
    path = "${snowflake-model}"
    query-prefix = "query: "
    document-prefix = ""
  '';
in
{
  test = pkgs.writeText "maneater-test.toml" snowflakeStanza;
  base = pkgs.writeText "maneater.toml" ''
    default = "snowflake"

    ${snowflakeStanza}
    # No [[corpora]] entries: maneater's synthesized default
    # activates a `type = "command"` manpages corpus that shells
    # out to maneater-man. See internal/charlie/commands.defaultManpagesCorpusConfig.
  '';
}
