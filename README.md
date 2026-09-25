# ridge-test

A disposable [furrow](https://github.com/akira-toriyama/furrow) board for
verifying [ridge](https://github.com/akira-toriyama/ridge) against the real
furrow CLI at working scale, without dirtying a real board.

`.furrow/` is a committed store regenerated from
[furrow-test](https://github.com/akira-toriyama/furrow-test)'s seed: the
one-day restaurant — five boxes that wait on one another, a hundred tasks,
181 dependency edges, 95 dated tasks, 4 in `waiting`, 7 repeating,
checklists with ticked rows, `file:` refs, epic meta. `seed/epics.ndjson`
and `seed/tasks.ndjson` are furrow-test's files copied verbatim: this repo
is a copy, not a fork. The story is edited in furrow-test and copied here
again; `git log -1 -- seed/tasks.ndjson` names the copy, and its body the
furrow-test commit it came from.

`seed/rebuild.sh` adds one thing on top of the story, because ridge's box
views and the satisfied epic dep need a shape the restaurant never reaches:
a closed box (開催を決めて日程と参加者を固める) that the venue box waits
on. It has no members, so the task count stays furrow-test's.

Ids and timestamps are minted on every regeneration, so nothing here is
byte-comparable with furrow-test or with ridge's `-dump` goldens (those are
the built-in fixture's); what carries over is the shape.

## Point ridge at it

The repo-local `.furrow` wins furrow's store discovery from this directory
(measured 2026-09-25: `ridge -dump -live` from this cwd renders this board),
so:

```sh
cd ridge-test
ridge                          # the real store, interactive
ridge -dump -live -plain       # one frame of the real store, no TTY
FURROW_DIR=$PWD/.furrow ridge  # the same store from any cwd
```

`-demo` is fixture-only and refused with `-live`.

## Reset after an experiment

The `baseline` tag marks the freshly regenerated store. This puts the
working tree back exactly — tracked shards restored, minted ones removed
(measured 2026-09-25 after an `add` and a `done`: `git status` empty,
`git diff baseline` empty):

```sh
git restore --source=baseline --staged --worktree -- .furrow && git clean -fdq .furrow
```

If `furrow sync` already pushed the mutations, commit that restore and sync
again rather than resetting the branch onto the tag: `main` also carries the
fleet files, and a force-push is the heavier tool.

## Regenerate

Needed when furrow-test's seed changes, or when a furrow release changes
the store layout beyond what `furrow upgrade` covers.

```sh
cp ../furrow-test/seed/epics.ndjson ../furrow-test/seed/tasks.ndjson seed/
sh seed/rebuild.sh   # empties tasks/ bodies/ epics/, recreates the boxes, add --batch, the closed box
furrow sync          # commits the staged deletions with the new shards and pushes
git tag -f baseline && git push -f origin baseline
```

`.furrow/config.toml` is furrow-test's with `default_repo` pointed at this
repo (the `waiting` lane, `timezone`, `provenance_markers` come with it);
`meta.json` is what `furrow init` wrote and `furrow upgrade` raises. A
layout change is `git rm -r .furrow && furrow init`, the config copy, then
the steps above. `furrow lint` is red on four dated rows by design (the
scenario's own overdue dates); rebuild.sh prints it and goes on.

## What this repo is not

- Not a place for test code: ridge's tests live in ridge; this is only the
  store they open.
- Not furrow-test: no drills, no `notes/`, no board rules. A gap found here
  is filed on the projects board — against furrow or against ridge — never
  as a task on this board.
