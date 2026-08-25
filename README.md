# ridge-test

A sandbox [furrow](https://github.com/akira-toriyama/furrow) store for
verifying [ridge](https://github.com/akira-toriyama/ridge)'s write path
against the real furrow CLI without dirtying a real board.

`.furrow/` is a committed, disposable furrow store that structurally mirrors
ridge's memstore fixture (the Kyushu camping trip board: 33 tasks, 5 epics —
one closed, one active, one stuck, epic deps included; snapshot of ridge's
`internal/store/memstore/fixture.go` at ridge commit 3225b0c). Task/epic ids
and timestamps are furrow-assigned, so they differ from the fixture — ridge's
`-dump` goldens are NOT byte-reusable here; what carries over is the
structure: lanes, priorities, deps, epic wiring, checklists, CJK-heavy
titles and bodies (with `[[id]]` links rewritten to this store's ids).

## Point ridge at it

```sh
FURROW_DIR=$PWD/.furrow ridge
```

or run ridge with this directory as cwd — the repo-local `.furrow` wins
furrow's store resolution over the user-level board config (measured
2026-08-25: `furrow board` from this cwd reports this store, not the central
board).

## Reset after an experiment

```sh
git reset --hard && git clean -fd .furrow
```

If `furrow sync` pushed mutations, restore the seeded baseline:

```sh
git reset --hard baseline && git push --force origin main
```

The `baseline` tag marks the freshly seeded store.

## Reseed

Only needed when a furrow release changes the store format in a way
`furrow upgrade` doesn't cover, or to rebuild from scratch:

```sh
go run ./cmd/seed -force   # drives the furrow CLI; ids come out fresh
```

The seeder verifies the result against `seed/board.json` (lane counts,
priorities, deps, epic progress/active/standing/pinned, open-dep resolution)
and prints the fixture→store id map. Commit the result and move the
`baseline` tag.

`seed/board.json` is the fixture snapshot, exported by marshalling
`memstore.New().Board()` from a throwaway `cmd/` program inside the ridge
module (the fixture is `internal/`, so the exporter must live there). The
closed epic (`e-2b7h` in the fixture) is appended by hand — it is absent
from the fixture's open-only epic read, but the seeder must create it open,
dep it, and close it so the dep resolves away the way furrow models it.

## What this repo is not

- Not a place for test code. ridge's e2e tests (synthetic SGR bytes through
  `tea.WithInput`) live in ridge — they import ridge internals; this repo is
  only the disposable store they point at.
- Not synced with ridge's fixture. If the fixture is re-themed or restructured,
  re-export and reseed deliberately; nothing tracks it automatically.
