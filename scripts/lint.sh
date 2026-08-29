#!/bin/sh
set -eu

repository=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version_file="$repository/.golangci-lint-version"
linter="$repository/.tools/bin/golangci-lint"

version=$(tr -d '[:space:]' < "$version_file")
case "$version" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *)
    printf 'invalid golangci-lint version: %s\n' "$version" >&2
    exit 1
    ;;
esac
version_number=${version#v}

CACHE_HOME="${XDG_CACHE_HOME:-$HOME/.cache}"
CACHE_DIR="$CACHE_HOME/review-party/tools"

temporary_dir=
archive_temporary=
cleanup() {
  if [ -n "$archive_temporary" ]; then
    rm -f "$archive_temporary"
  fi
  if [ -n "$temporary_dir" ]; then
    rm -rf "$temporary_dir"
  fi
}

trap cleanup EXIT HUP INT TERM

version_matches() {
  [ -x "$linter" ] || return 1
  actual=$("$linter" version 2>/dev/null) || return 1
  actual_version=$(printf '%s\n' "$actual" | sed -n '1s/.*version \([^ ]*\).*/\1/p')
  [ "$actual_version" = "$version_number" ]
}

download_file() {
  destination=$1
  url=$2
  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --silent --show-error --retry 3 --output "$destination" "$url"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget --output-document="$destination" "$url"
    return
  fi
  printf 'lint bootstrap requires curl or wget\n' >&2
  exit 1
}

checksum_for() {
  archive=$1
  if [ "$host_os" = Linux ]; then
    sha256sum "$archive" | awk '{print $1}'
  else
    shasum -a 256 "$archive" | awk '{print $1}'
  fi
}

verify_archive() {
  archive=$1
  expected=$(awk -v name="$archive_name" '$2 == name {print $1; exit}' "$checksums_file")
  if [ -z "$expected" ]; then
    printf 'checksum is missing for %s\n' "$archive_name" >&2
    return 1
  fi
  actual=$(checksum_for "$archive") || return 1
  if [ "$actual" != "$expected" ]; then
    printf 'checksum mismatch for %s\n' "$archive" >&2
    return 1
  fi
}

bootstrap_linter() {
  if version_matches; then
    return
  fi

  host_os_name=$(uname -s)
  case "$host_os_name" in
    Linux)
      host_os=Linux
      release_os=linux
      checksum_command=sha256sum
      ;;
    Darwin)
      host_os=Darwin
      release_os=darwin
      checksum_command=shasum
      ;;
    *)
      printf 'unsupported operating system: %s\n' "$host_os_name" >&2
      exit 1
      ;;
  esac
  if ! command -v "$checksum_command" >/dev/null 2>&1; then
    printf 'lint bootstrap requires %s\n' "$checksum_command" >&2
    exit 1
  fi

  host_arch_name=$(uname -m)
  case "$host_arch_name" in
    amd64|x86_64)
      release_arch=amd64
      ;;
    arm64|aarch64)
      release_arch=arm64
      ;;
    *)
      printf 'unsupported architecture: %s\n' "$host_arch_name" >&2
      exit 1
      ;;
  esac

  archive_name="golangci-lint-${version_number}-${release_os}-${release_arch}.tar.gz"
  archive_root="golangci-lint-${version_number}-${release_os}-${release_arch}"
  cache_archive="$CACHE_DIR/$archive_name"
  release_url="https://github.com/golangci/golangci-lint/releases/download/$version"

  mkdir -p "$CACHE_DIR"
  temporary_dir=$(mktemp -d "${TMPDIR:-/tmp}/review-party-lint.XXXXXX")
  checksums_file="$temporary_dir/checksums.txt"
  download_file "$checksums_file" "$release_url/golangci-lint-${version_number}-checksums.txt"

  if [ ! -f "$cache_archive" ] || ! verify_archive "$cache_archive"; then
    archive_temporary=$(mktemp "$CACHE_DIR/.golangci-lint-download.XXXXXX")
    download_file "$archive_temporary" "$release_url/$archive_name"
    verify_archive "$archive_temporary"
    mv -f "$archive_temporary" "$cache_archive"
    archive_temporary=
  fi
  verify_archive "$cache_archive"

  if version_matches; then
    return
  fi

  extract_dir=$(mktemp -d "$temporary_dir/extract.XXXXXX")
  tar -xzf "$cache_archive" -C "$extract_dir"
  extracted_linter="$extract_dir/$archive_root/golangci-lint"
  if [ ! -f "$extracted_linter" ]; then
    printf 'archive does not contain %s\n' "$archive_root/golangci-lint" >&2
    exit 1
  fi

  tool_dir="$repository/.tools/bin"
  mkdir -p "$tool_dir"
  installed_temporary=$(mktemp "$tool_dir/.golangci-lint.XXXXXX")
  cp "$extracted_linter" "$installed_temporary"
  chmod 0755 "$installed_temporary"
  mv -f "$installed_temporary" "$linter"

  if ! version_matches; then
    printf 'installed golangci-lint does not report version %s\n' "$version_number" >&2
    exit 1
  fi
}

now_stamp() {
  stamp=$(date +%s%N 2>/dev/null) || {
    date +%s
    return
  }
  case "$stamp" in
    *[!0-9]*) date +%s ;;
    *) printf '%s\n' "$stamp" ;;
  esac
}

format_elapsed() {
  start=$1
  end=$2
  start_len=${#start}
  end_len=${#end}
  if [ "$start_len" -ge 18 ] && [ "$end_len" -ge 18 ]; then
    ms=$(((end - start) / 1000000))
    if [ "$ms" -lt 1000 ]; then
      printf '%dms' "$ms"
      return
    fi
    awk -v ms="$ms" 'BEGIN { printf "%.1fs", ms / 1000 }'
    return
  fi
  seconds=$((end - start))
  if [ "$seconds" -lt 1 ]; then
    printf '<1s'
    return
  fi
  printf '%ds' "$seconds"
}

count_packages() {
  go list "$@" | wc -l | tr -d ' '
}

count_linters() {
  "$linter" linters 2>/dev/null | awk '
    /^Enabled by your configuration linters:/ { enabled = 1; next }
    /^Disabled by your configuration linters:/ { exit }
    enabled && /^[a-z]/ { count++ }
    END { print count + 0 }
  '
}

cd "$repository"
bootstrap_linter
./scripts/verify-lint-suppressions.sh

if [ "$#" -eq 0 ]; then
  set -- ./...
fi

package_count=$(count_packages "$@")
linter_count=$(count_linters)
started=$(now_stamp)
set +e
lint_output=$("$linter" run "$@" 2>&1)
lint_status=$?
set -e
elapsed=$(format_elapsed "$started" "$(now_stamp)")

if [ -n "$lint_output" ]; then
  printf '%s\n' "$lint_output" | grep -vE '^0 issues\.$|^level=warning msg="\[runner/exclusion_rules\] Skipped 0 issues' || true
fi

if [ "$lint_status" -ne 0 ]; then
  exit "$lint_status"
fi

if [ "$package_count" -eq 1 ]; then
  package_label='package'
else
  package_label='packages'
fi
if [ "$linter_count" -eq 1 ]; then
  linter_label='linter'
else
  linter_label='linters'
fi
printf 'ok golangci-lint %s  %s %s  %s %s  0 issues  %s\n' \
  "$version" "$package_count" "$package_label" "$linter_count" "$linter_label" "$elapsed"
printf 'ok suppressions\n'
