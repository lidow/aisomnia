#!/bin/sh
# Thin wrapper so the one-time setup is discoverable: ./scripts/set-owner.sh <github account>
set -eu
[ $# -eq 1 ] || { echo "usage: $0 <github account>" >&2; exit 2; }
exec make -C "$(dirname "$0")/.." set-owner OWNER="$1"
