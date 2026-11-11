#!/bin/sh
# Retain the original CLI parser and WSL environment bridge, not a second Git
# implementation. cli.js clears ELECTRON_RUN_AS_NODE before starting MyGo.
STATIC="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ELECTRON="$STATIC/../../electron.exe"
CLI="$STATIC/../cli.js"
if grep -qi microsoft /proc/version 2>/dev/null; then
  if ! command -v wslpath >/dev/null 2>&1; then
    echo 'Gitowl CLI requires WSL with wslpath support.' >&2
    exit 1
  fi
  CLI=$(wslpath -m "$CLI")
  export WSLENV=ELECTRON_RUN_AS_NODE/w:$WSLENV
elif [ "$(uname -s | cut -c 1-9)" = CYGWIN_NT ]; then
  CLI=$(cygpath -m "$CLI")
fi
ELECTRON_RUN_AS_NODE=1 "$ELECTRON" "$CLI" "$@"
exit $?
