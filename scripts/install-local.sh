#!/bin/sh
set -eu

repository=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
install_dir=${1:-${XDG_BIN_HOME:-"$HOME/.local/bin"}}
target="$install_dir/review-party"

mkdir -p "$install_dir"
temporary=$(mktemp "$install_dir/.review-party.XXXXXX")
cleanup() {
  rm -f "$temporary"
}
trap cleanup EXIT HUP INT TERM

(
  cd "$repository"
  go build -trimpath -o "$temporary" ./cmd/review-party
)
chmod 0755 "$temporary"
mv -f "$temporary" "$target"
trap - EXIT HUP INT TERM

printf 'Installed Review Party at %s\n' "$target"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) printf 'Warning: %s is not in PATH.\n' "$install_dir" >&2 ;;
esac
"$target" version
