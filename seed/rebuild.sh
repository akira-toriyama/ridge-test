#!/bin/sh
# seed/rebuild.sh — regenerate .furrow/ from seed/. epics.ndjson and tasks.ndjson
# are furrow-test's files copied verbatim (the story is edited there, never
# here). The one ridge-test addition is the closed box at the end: ridge's box
# views and the satisfied epic dep need one and the restaurant story has none;
# it has no members, so the task count stays furrow-test's. Every id changes on
# each run. config.toml and meta.json are kept; the staged deletions and the
# new shards ride `furrow sync` together, and `baseline` then moves to that
# commit.
set -eu
cd "$(dirname "$0")/.."
git rm -rq --ignore-unmatch .furrow/tasks .furrow/bodies .furrow/epics
rm -rf .furrow/tasks .furrow/bodies .furrow/epics
mkdir -p .furrow/tasks .furrow/bodies .furrow/epics
jq -c . seed/epics.ndjson | while IFS= read -r line; do
  title=$(printf '%s' "$line" | jq -r .title)
  goal=$(printf '%s' "$line" | jq -r '.goal // empty')
  labels=$(printf '%s' "$line" | jq -r '.labels | join(",")')
  set -- furrow epic add "$title" --goal "$goal" -l "$labels"   # the board scope supplies the repo
  for kv in $(printf '%s' "$line" | jq -r '.meta | to_entries[] | "\(.key)=\(.value)"'); do
    set -- "$@" --meta "$kv"
  done
  "$@" >/dev/null
done
jq -c . seed/epics.ndjson | while IFS= read -r line; do
  title=$(printf '%s' "$line" | jq -r .title)
  printf '%s' "$line" | jq -r '.deps[]' | while IFS= read -r dep; do
    furrow epic dep "$title" "$dep" >/dev/null
  done
  if [ "$(printf '%s' "$line" | jq -r .active)" = true ]; then
    furrow epic activate "$title" >/dev/null
  fi
done
furrow add --batch seed/tasks.ndjson --json >/dev/null
# ridge-test only: the box the venue box waited on, already closed.
furrow epic add "開催を決めて日程と参加者を固める" --goal "開催日 2026-11-21 と参加者 12 名が固まっている" -l event,kickoff --meta slug=kickoff >/dev/null
furrow epic done "開催を決めて" >/dev/null
furrow epic dep "会場と日程を確定する" "開催を決めて" >/dev/null
furrow lint || true   # the scenario's dated rows are red by design; read, then sync
echo "rebuilt: $(furrow ls -n 0 --json | jq length) tasks — now: furrow sync"
