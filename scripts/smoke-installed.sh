#!/bin/sh
set -eu

repository=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
binary=${1:-review-party}
work="$repository/scratch/installed-smoke"

rm -rf "$work"
mkdir -p "$work/repository" "$work/config" "$work/state"
git -C "$work/repository" init -q
git -C "$work/repository" config user.email smoke@review-party.local
git -C "$work/repository" config user.name 'Review Party Smoke'
printf 'smoke\n' > "$work/repository/README.md"
git -C "$work/repository" add README.md
git -C "$work/repository" commit -qm 'smoke fixture'

(
  cd "$work/repository"
  XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state" "$binary" --help >/dev/null
  XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state" "$binary" version --format json
  XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state" "$binary" init --repo . </dev/null
)

printf 'Installed-binary smoke test passed outside the source repository.\n'
