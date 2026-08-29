#!/bin/sh
set -eu

repository=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
failed=0

# act copies the worktree without a usable Git dir and can leak host GIT_DIR.
# GitHub's checkout action still has a real repo. Prefer tracked files.
list_go_files() {
  unset GIT_DIR GIT_WORK_TREE
  if git -C "$repository" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git -C "$repository" ls-files -- '*.go'
    return
  fi
  (cd "$repository" && find . -name '*.go' -type f | sed 's|^\./||')
}

tracked_go_files=$(list_go_files)

if [ -n "$tracked_go_files" ]; then
  while IFS= read -r file; do
    case "$file" in
      internal/engine/testdata/evals/*) continue ;;
    esac
    [ -f "$repository/$file" ] || continue

    if grep -nE '//gocognit:ignore|//gocyclo:ignore|//exhaustive:ignore|//lint:ignore|//lint:file-ignore|//nolint:all|#nosec' "$repository/$file"; then
      failed=1
    fi
    if grep -nE '//nolint([^:]|$)' "$repository/$file"; then
      failed=1
    fi
  done <<EOF
$tracked_go_files
EOF
fi

if [ "$failed" -ne 0 ]; then
  printf 'unsupported lint suppressions found\n' >&2
  exit 1
fi
