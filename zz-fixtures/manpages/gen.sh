#!/usr/bin/env bash
# Regenerate the deterministic bench-manpath fixture pages.
#
# 15 man1 + 5 man5 roff pages with distinct NAME / SYNOPSIS / DESCRIPTION
# content so mandoc + pandoc have real work to do per page without
# depending on whatever the host system has installed. The output is
# stable across runs — content is derived only from the name list and
# the per-page index.
#
# Invocation: `just gen-manpages-fixtures` (or run this script directly).
# The justfile recipe sets CWD to the repo root; running the script
# standalone works from any CWD because paths are relative to the
# script's own directory.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"

NAMES1=(alfabench bravobench charliebench deltabench echobench foxbench
  golfbench hotelbench indiabench julietbench kilobench limabench
  mikebench novemberbench oscarbench)

for i in "${!NAMES1[@]}"; do
  n="${NAMES1[$i]}"
  idx=$((i + 1))
  UPPER=$(echo "$n" | tr '[:lower:]' '[:upper:]')
  cat >"man1/$n.1" <<EOF
.TH $UPPER 1 "2026-04-19" "maneater-bench" "Maneater Bench Fixtures"
.SH NAME
$n \\- deterministic bench fixture command number $idx
.SH SYNOPSIS
.B $n
[\\fIOPTION\\fR]... [\\fIFILE\\fR]...
.SH DESCRIPTION
The
.B $n
utility is fixture number $idx in the bench-manpath corpus. It exists to
exercise the mandoc + pandoc rendering pipeline with deterministic roff
source so wall-clock measurements are reproducible across runs of the
bench recipe. Each fixture page has distinct NAME / SYNOPSIS /
DESCRIPTION content so the embedding index does not collapse them to a
single entry.
.SH OPTIONS
.TP
.BR \\-v " " \\-\\-verbose
Print verbose output for fixture $idx.
.TP
.BR \\-q " " \\-\\-quiet
Suppress non-essential output for fixture $idx.
.SH EXAMPLES
.PP
Exercise fixture $idx in default mode:
.PP
.nf
.RS
$n alpha.txt bravo.txt
.RE
.fi
.SH SEE ALSO
.BR maneater (1)
EOF
done

NAMES5=(alfaconf bravoconf charlieconf deltaconf echoconf)
for i in "${!NAMES5[@]}"; do
  n="${NAMES5[$i]}"
  idx=$((i + 1))
  UPPER=$(echo "$n" | tr '[:lower:]' '[:upper:]')
  cat >"man5/$n.5" <<EOF
.TH $UPPER 5 "2026-04-19" "maneater-bench" "Maneater Bench Fixtures"
.SH NAME
$n \\- deterministic bench fixture config format number $idx
.SH SYNOPSIS
.I ~/.config/maneater-bench/$n.toml
.SH DESCRIPTION
This page describes bench fixture config format number $idx. The format
is a TOML document whose shape varies per fixture so the embedding index
does not collapse across pages. Mandoc + pandoc must still render all
the usual sections (NAME, SYNOPSIS, DESCRIPTION, EXAMPLES, SEE ALSO)
per page.
.SH SYNTAX
.PP
Fields:
.TP
.B enabled
Boolean for fixture $idx.
.TP
.B label
String label for fixture $idx.
.SH EXAMPLES
.PP
.nf
.RS
enabled = true
label = "fixture-$idx"
.RE
.fi
.SH SEE ALSO
.BR maneater.toml (5)
EOF
done

echo "regenerated $((${#NAMES1[@]} + ${#NAMES5[@]})) fixture pages in $HERE"
