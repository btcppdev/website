#!/usr/bin/env sh
set -eu

dir="${1:-db/migrations}"
base_ref="${2:-}"

if [ ! -d "$dir" ]; then
  echo "migration directory not found: $dir" >&2
  exit 1
fi

tmp="${TMPDIR:-/tmp}/btcpp-migration-prefixes.$$"
new_tmp="$tmp.new"
trap 'rm -f "$tmp" "$new_tmp"' EXIT HUP INT TERM

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

check_newer_than_ref() {
  ref=$1
  comparison=$2

  if ! git cat-file -e "$ref^{commit}" 2>/dev/null; then
    echo "migration comparison ref not found: $ref" >&2
    exit 1
  fi

  highest=$(git ls-tree -r --name-only "$ref" -- "$dir" | awk -F/ '
    {
      name = $NF
      if (name !~ /^[0-9]+_.+\.sql$/) next
      sub(/_.*/, "", name)
      if ((name + 0) > max) max = name + 0
    }
    END { print max + 0 }
  ')

  [ "$highest" -gt 0 ] || return 0

  git diff $comparison --diff-filter=A --name-only -- "$dir" > "$new_tmp"
  failed=0
  while IFS= read -r file; do
    case "$file" in
      "$dir"/*.sql)
        name=$(basename "$file")
        prefix=${name%%_*}
        case "$prefix" in
          ''|*[!0-9]*) continue ;;
        esac
        prefix_number=$(awk -v value="$prefix" 'BEGIN { print value + 0 }')
        if [ "$prefix_number" -le "$highest" ]; then
          echo "new migration $file uses version $prefix, but $ref already contains version $(printf '%03d' "$highest")" >&2
          failed=1
        fi
        ;;
    esac
  done < "$new_tmp"
  return "$failed"
}

if [ -n "$base_ref" ]; then
  check_newer_than_ref "$base_ref" "$base_ref...HEAD"
elif git rev-parse --verify HEAD >/dev/null 2>&1; then
  check_newer_than_ref HEAD "--cached HEAD"
fi
