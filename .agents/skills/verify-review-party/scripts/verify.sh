#!/bin/sh
set -eu

repository=$(CDPATH= cd -- "$(dirname -- "$0")/../../../.." && pwd)
scratch_root="$repository/scratch/verify-review-party"

fail() {
  printf 'verify-review-party: %s\n' "$*" >&2
  exit 1
}

digest() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail 'sha256sum or shasum is required'
  fi
}

validate_run_dir() {
  [ -n "${1:-}" ] || fail 'run directory is required'
  case "$1" in
    */../*|*/..) fail 'run directory must not contain .. segments' ;;
    "$scratch_root"/*) ;;
    *) fail "run directory is outside $scratch_root" ;;
  esac
  [ ! -L "$1" ] || fail 'run directory must not be a symlink'
  [ -f "$1/ownership.tsv" ] || fail "ownership record is missing at $1/ownership.tsv"
}

manifest_value() {
  key=$1
  file=$2
  awk -F '\t' -v key="$key" '$1 == key { print $2; found = 1; exit } END { if (!found) exit 1 }' "$file"
}

# Hashes the full working tree, including uncommitted and untracked
# non-ignored files, without touching the real index.
source_tree() {
  source_repository=$1
  git_dir=$(git -C "$source_repository" rev-parse --absolute-git-dir)
  index=$(mktemp)
  if [ -f "$git_dir/index" ]; then cp "$git_dir/index" "$index"; else rm -f "$index"; fi
  tree=$(GIT_INDEX_FILE="$index" git -C "$source_repository" add -A 2>/dev/null &&
    GIT_INDEX_FILE="$index" git -C "$source_repository" write-tree) || { rm -f "$index"; fail 'cannot hash the source tree'; }
  rm -f "$index"
  printf '%s\n' "$tree"
}

launch() {
  mkdir -p "$scratch_root"
  run_id=${VERIFY_REVIEW_PARTY_RUN_ID:-"$(date -u +%Y%m%dT%H%M%SZ)-$$"}
  case "$run_id" in
    *[!A-Za-z0-9._-]*|'') fail 'run identity may contain only letters, digits, dot, _, and hyphen' ;;
  esac
  run_dir="$scratch_root/$run_id"
  [ ! -e "$run_dir" ] || fail "run already exists at $run_dir"

  runtime="$run_dir/runtime"
  binary_dir="$runtime/bin"
  config_root="$runtime/config"
  state_root="$runtime/state"
  home="$runtime/home"
  target_repository="$runtime/repository"
  evidence="$run_dir/evidence"
  mkdir -p "$binary_dir" "$config_root" "$state_root" "$home" "$target_repository" "$evidence"

  tree_before_build=$(source_tree "$repository")
  PATH="$binary_dir:$PATH" "$repository/scripts/install-local.sh" "$binary_dir" >/dev/null
  built_tree=$(source_tree "$repository")
  [ "$built_tree" = "$tree_before_build" ] || fail 'source tree changed during the build; launch again'
  git -C "$target_repository" init -q
  git -C "$target_repository" config user.email verify@review-party.local
  git -C "$target_repository" config user.name 'Review Party Verifier'
  printf 'verification fixture\n' > "$target_repository/README.md"
  git -C "$target_repository" add README.md
  git -C "$target_repository" commit -qm 'verification fixture'

  source_revision=$(git -C "$repository" rev-parse HEAD)
  if [ "$built_tree" = "$(git -C "$repository" rev-parse 'HEAD^{tree}')" ]; then
    source_dirty=no
  else
    source_dirty=yes
  fi
  binary="$binary_dir/review-party"
  binary_digest=$(digest "$binary")
  config_file="$config_root/review-party/config.json"
  state_directory="$state_root/review-party"

  {
    printf 'run_identity\t%s\n' "$run_id"
    printf 'repository_root\t%s\n' "$repository"
    printf 'source_revision\t%s\n' "$source_revision"
    printf 'source_tree\t%s\n' "$built_tree"
    printf 'source_dirty\t%s\n' "$source_dirty"
    printf 'binary\t%s\n' "$binary"
    printf 'binary_sha256\t%s\n' "$binary_digest"
    printf 'config_root\t%s\n' "$config_root"
    printf 'config_file\t%s\n' "$config_file"
    printf 'state_root\t%s\n' "$state_root"
    printf 'state_directory\t%s\n' "$state_directory"
    printf 'home\t%s\n' "$home"
    printf 'target_repository\t%s\n' "$target_repository"
    printf 'evidence_directory\t%s\n' "$evidence"
    printf 'terminal_session\tverify-review-party-%s\n' "$(printf '%s' "$run_id" | tr '.' '_')"
  } > "$run_dir/ownership.tsv"
  printf '%s\n' "$run_id" > "$config_root/verify-review-party.owner"

  HOME="$home" XDG_CONFIG_HOME="$config_root" XDG_STATE_HOME="$state_root" \
    "$binary" init --repo "$target_repository" --state-dir "$state_directory" --config "$config_file" >/dev/null
  doctor "$run_dir" >/dev/null
  printf '%s\n' "$run_dir"
}

doctor() {
  run_dir=$1
  validate_run_dir "$run_dir"
  manifest="$run_dir/ownership.tsv"
  recorded_repository=$(manifest_value repository_root "$manifest")
  recorded_revision=$(manifest_value source_revision "$manifest")
  expected_revision=${VERIFY_REVIEW_PARTY_EXPECTED_REVISION:-$recorded_revision}
  current_revision=$(git -C "$recorded_repository" rev-parse HEAD 2>/dev/null) || fail 'cannot read the current source revision'
  [ "$current_revision" = "$expected_revision" ] || fail "source revision mismatch: expected $expected_revision, got $current_revision"
  recorded_tree=$(manifest_value source_tree "$manifest")
  [ "$(source_tree "$recorded_repository")" = "$recorded_tree" ] || fail 'source tree mismatch: the working tree changed since launch; launch a fresh run'

  binary=$(manifest_value binary "$manifest")
  [ -x "$binary" ] || fail "owned binary is missing or not executable at $binary"
  expected_digest=$(manifest_value binary_sha256 "$manifest")
  actual_digest=$(digest "$binary")
  [ "$actual_digest" = "$expected_digest" ] || fail 'owned binary digest mismatch'
  "$binary" version 2>/dev/null | grep -q '^Review Party ' || fail 'owned binary does not identify as Review Party'

  config_root=$(manifest_value config_root "$manifest")
  run_identity=$(manifest_value run_identity "$manifest")
  [ -d "$config_root" ] || fail "owned configuration root is missing at $config_root"
  [ "$(sed -n '1p' "$config_root/verify-review-party.owner" 2>/dev/null || true)" = "$run_identity" ] || fail 'configuration ownership marker mismatch'
  state_root=$(manifest_value state_root "$manifest")
  [ -d "$state_root" ] || fail "owned state root is missing at $state_root"
  state_directory=$(manifest_value state_directory "$manifest")
  [ -f "$state_directory/ledger.sqlite" ] || fail "owned state database is missing at $state_directory/ledger.sqlite"
  home=$(manifest_value home "$manifest")
  [ -d "$home" ] || fail "owned home is missing at $home"
  target_repository=$(manifest_value target_repository "$manifest")
  git -C "$target_repository" rev-parse --show-toplevel >/dev/null 2>&1 || fail "owned target is not a Git repository at $target_repository"

  printf 'doctor: ready\n'
}

capture() {
  run_dir=$1
  evidence_relative=$2
  shift 2
  [ "${1:-}" = '--' ] || fail 'capture requires -- before the command'
  shift
  [ "$#" -gt 0 ] || fail 'capture requires a command'
  validate_run_dir "$run_dir"
  case "$evidence_relative" in
    /*|../*|*/../*|*/..) fail 'evidence path must stay inside the run evidence directory' ;;
  esac

  manifest="$run_dir/ownership.tsv"
  evidence_root=$(manifest_value evidence_directory "$manifest")
  output="$evidence_root/$evidence_relative"
  mkdir -p "$(dirname -- "$output")"
  stdout="$output.stdout"
  stderr="$output.stderr"
  status_file="$output.exit"
  arguments_file="$output.args"
  : > "$arguments_file"
  for argument in "$@"; do
    printf '%s\n' "$argument" >> "$arguments_file"
  done

  binary=$(manifest_value binary "$manifest")
  if [ "$1" = 'review-party' ]; then
    shift
    set -- "$binary" "$@"
  fi
  config_root=$(manifest_value config_root "$manifest")
  state_root=$(manifest_value state_root "$manifest")
  home=$(manifest_value home "$manifest")
  set +e
  HOME="$home" XDG_CONFIG_HOME="$config_root" XDG_STATE_HOME="$state_root" "$@" >"$stdout" 2>"$stderr"
  status=$?
  set -e
  printf '%s\n' "$status" > "$status_file"
  {
    printf 'arguments: %s\n' "$arguments_file"
    printf 'stdout: %s\n' "$stdout"
    printf 'stderr: %s\n' "$stderr"
    printf 'exit: %s\n' "$status"
  } > "$output"
  return "$status"
}

manifest_path() {
  validate_run_dir "$1"
  manifest_value "$2" "$1/ownership.tsv" || fail "manifest has no $2 entry"
}

cleanup() {
  run_dir=$1
  validate_run_dir "$run_dir"
  runtime="$run_dir/runtime"
  if [ -e "$runtime" ]; then
    [ ! -L "$runtime" ] || fail 'runtime directory must not be a symlink'
    rm -rf "$runtime"
  fi
  printf 'cleanup: complete\n'
}

case "${1:-}" in
  launch) [ "$#" -eq 1 ] || fail 'usage: verify.sh launch'; launch ;;
  doctor) [ "$#" -eq 2 ] || fail 'usage: verify.sh doctor RUN_DIR'; doctor "$2" ;;
  path) [ "$#" -eq 3 ] || fail 'usage: verify.sh path RUN_DIR KEY'; manifest_path "$2" "$3" ;;
  capture) [ "$#" -ge 5 ] || fail 'usage: verify.sh capture RUN_DIR EVIDENCE -- COMMAND [ARGS...]'; shift; capture "$@" ;;
  cleanup) [ "$#" -eq 2 ] || fail 'usage: verify.sh cleanup RUN_DIR'; cleanup "$2" ;;
  *) fail 'usage: verify.sh launch | doctor RUN_DIR | path RUN_DIR KEY | capture RUN_DIR EVIDENCE -- COMMAND [ARGS...] | cleanup RUN_DIR' ;;
esac
