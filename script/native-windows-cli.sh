#!/bin/sh
# Retain the original CLI parser and WSL environment bridge, not a second Git
# implementation. Node runs cli.js and starts the single MyGo owner.
STATIC="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
NODE="$STATIC/../node.exe"
CLI="$STATIC/../cli.js"
if grep -qi microsoft /proc/version 2>/dev/null; then
  if ! command -v wslpath >/dev/null 2>&1; then
    echo 'Gitowl CLI requires WSL with wslpath support.' >&2
    exit 1
  fi
  CLI=$(wslpath -m "$CLI")
elif [ "$(uname -s | cut -c 1-9)" = CYGWIN_NT ]; then
  CLI=$(cygpath -m "$CLI")
fi
"$NODE" "$CLI" "$@"
exit $?
