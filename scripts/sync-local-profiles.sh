#!/bin/sh
set -eu

repository=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
target_root=${1:-${XDG_CONFIG_HOME:-"$HOME/.config"}/review-party/profiles}
reviewer=${REVIEW_PARTY_DOGFOOD_REVIEWER:-opencode}
model=${REVIEW_PARTY_DOGFOOD_MODEL:-meta/muse-spark-1.2-contributor}
effort=${REVIEW_PARTY_DOGFOOD_EFFORT:-high}
deadline=${REVIEW_PARTY_DOGFOOD_DEADLINE:-3m}
revision_source="$repository/internal/engine/profile.go"

revision_for() {
  awk -F '"' -v name="$1" '$2 == name { print $4; exit }' "$revision_source"
}

for profile in bugs code-quality documentation; do
  instructions="$repository/profiles/$profile.md"
  revision=$(revision_for "$profile")
  if [ ! -f "$instructions" ] || [ -z "$revision" ]; then
    printf 'Cannot resolve packaged template %s and its revision.\n' "$profile" >&2
    exit 1
  fi

  directory="$target_root/$profile"
  install -d -m 0700 "$directory"
  metadata=$(mktemp "$directory/.profile.json.XXXXXX")
  content=$(mktemp "$directory/.instructions.md.XXXXXX")
  cleanup() {
    rm -f "$metadata" "$content"
  }
  trap cleanup EXIT HUP INT TERM

  cat > "$metadata" <<EOF
{
  "schema_version": 1,
  "name": "$profile",
  "reviewer": "$reviewer",
  "model": "$model",
  "reasoning_effort": "$effort",
  "attempt_deadline": "$deadline",
  "template_id": "$profile",
  "template_revision": "$revision"
}
EOF
  cp "$instructions" "$content"
  chmod 0600 "$metadata" "$content"
  mv -f "$content" "$directory/instructions.md"
  mv -f "$metadata" "$directory/profile.json"
  trap - EXIT HUP INT TERM
  printf 'Synced global Profile %s at %s\n' "$profile" "$directory"
done

printf 'Reviewer: %s\nModel: %s\nEffort: %s\nAttempt deadline: %s\n' \
  "$reviewer" "$model" "$effort" "$deadline"
