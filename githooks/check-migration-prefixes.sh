#!/usr/bin/env sh
set -eu

dir="${1:-db/migrations}"

if [ ! -d "$dir" ]; then
  echo "migration directory not found: $dir" >&2
  exit 1
fi

tmp="${TMPDIR:-/tmp}/btcpp-migration-prefixes.$$"
trap 'rm -f "$tmp"' EXIT HUP INT TERM

found=0
for file in "$dir"/*.sql; do
  [ -e "$file" ] || continue
  name=$(basename "$file")
  prefix=${name%%_*}
  case "$prefix" in
    ''|*[!0-9]*)
      echo "migration file must start with a numeric prefix followed by _: $file" >&2
      exit 1
      ;;
  esac
  printf '%s %s\n' "$prefix" "$file" >> "$tmp"
  found=1
done

if [ "$found" -eq 0 ]; then
  exit 0
fi

duplicates=$(sort "$tmp" | awk '
  prev == $1 {
    if (!printed[$1]) {
      print first[$1]
      printed[$1] = 1
    }
    print $0
  }
  prev != $1 {
    prev = $1
    first[$1] = $0
  }
')

if [ -n "$duplicates" ]; then
  echo "duplicate migration version prefix found in $dir:" >&2
  echo "$duplicates" >&2
  echo "Each db/migrations/*.sql file must use a unique numeric prefix, e.g. 002_name.sql." >&2
  exit 1
fi
