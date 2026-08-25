// seed rebuilds the sandbox .furrow store from seed/board.json by driving the
// furrow CLI — never by writing furrow-owned files (tasks/*.json, meta.json)
// directly. Task/epic ids and timestamps are furrow-assigned, so they differ
// from the source fixture on every reseed; the STRUCTURE (titles, lanes,
// priorities, deps, epic wiring, bodies with rewritten [[id]] links) is what
// is reproduced. The committed store is the stable baseline — reseeding is
// for rebuilding after a furrow schema change, not for routine resets (those
// are `git reset --hard`).
//
// Every furrow invocation carries FURROW_DIR pinned to the target store, and
// the store path is read back via `furrow board --json` before the first
// mutation — this program must never be able to write to a real board.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type checklistItem struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type task struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Status    string          `json:"status"`
	Priority  int             `json:"priority"`
	Value     int             `json:"value"`
	Effort    int             `json:"effort"`
	Labels    []string        `json:"labels"`
	Repos     []string        `json:"repos"`
	Epic      string          `json:"epic"`
	Deps      []string        `json:"deps"`
	Refs      []string        `json:"refs"`
	Checklist []checklistItem `json:"checklist"`
	Due       *time.Time      `json:"due"`
	Body      string          `json:"body"`
}

type epic struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Goal     string            `json:"goal"`
	Active   bool              `json:"active"`
	Standing bool              `json:"standing"`
	Pinned   bool              `json:"pinned"`
	Labels   []string          `json:"labels"`
	Repos    []string          `json:"repos"`
	Meta     map[string]string `json:"meta"`
	Deps     []string          `json:"deps"`
	Closed   bool              `json:"closed"`
}

type lane struct {
	Name string `json:"name"`
	Next bool   `json:"next"`
	Done bool   `json:"done"`
}

type seedData struct {
	Lanes []lane `json:"lanes"`
	Epics []epic `json:"epics"`
	Tasks []task `json:"tasks"`
}

var storeDir string

func furrow(args ...string) ([]byte, error) {
	cmd := exec.Command("furrow", args...)
	cmd.Env = append(os.Environ(), "FURROW_DIR="+storeDir)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("furrow %s: %w\n%s", strings.Join(args, " "), err, errb.String())
	}
	return []byte(out.String()), nil
}

func must(out []byte, err error) []byte {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	return out
}

// mustID runs a furrow mutation with --json and returns the new entity's id.
func mustID(args ...string) string {
	out := must(furrow(args...))
	var row struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &row); err != nil || row.ID == "" {
		fmt.Fprintf(os.Stderr, "error: furrow %s: no id in reply %q\n", strings.Join(args, " "), out)
		os.Exit(1)
	}
	return row.ID
}

func main() {
	data := flag.String("data", "seed/board.json", "seed data (exported from ridge's memstore fixture)")
	store := flag.String("store", ".furrow", "target store directory (created; refuses to exist unless -force)")
	force := flag.Bool("force", false, "delete an existing target store first")
	flag.Parse()

	abs, err := filepath.Abs(*store)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	storeDir = abs

	raw, err := os.ReadFile(*data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	var seed seedData
	if err := json.Unmarshal(raw, &seed); err != nil {
		fmt.Fprintln(os.Stderr, "error: decoding seed data:", err)
		os.Exit(1)
	}

	if _, err := os.Stat(storeDir); err == nil {
		if !*force {
			fmt.Fprintf(os.Stderr, "error: %s exists — pass -force to rebuild it\n", storeDir)
			os.Exit(1)
		}
		if err := os.RemoveAll(storeDir); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}

	must(furrow("init"))
	patchConfig(seed.Lanes)

	// The path furrow will actually write to, read back through furrow itself.
	// A mismatch means environment resolution picked a different board — abort
	// before the first mutation, not after.
	var pre struct {
		Store string `json:"store"`
	}
	if err := json.Unmarshal(must(furrow("board", "--json")), &pre); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if pre.Store != storeDir {
		fmt.Fprintf(os.Stderr, "error: furrow resolved store %q, expected %q — refusing to seed\n", pre.Store, storeDir)
		os.Exit(1)
	}

	epicID := seedEpics(seed.Epics)
	taskID := seedTasks(seed.Tasks, epicID)
	writeBodies(seed.Tasks, taskID, epicID)
	wireDeps(seed.Tasks, taskID)
	checkItems(seed.Tasks, taskID)
	closeDone(seed.Tasks, taskID)
	finishEpics(seed.Epics, epicID)

	verify(seed, taskID, epicID)

	fmt.Printf("seeded %d tasks, %d epics into %s\n", len(seed.Tasks), len(seed.Epics), storeDir)
	fmt.Println("id map (fixture -> store):")
	for _, e := range seed.Epics {
		fmt.Printf("  %s -> %s\n", e.ID, epicID[e.ID])
	}
	for _, t := range seed.Tasks {
		fmt.Printf("  %s -> %s\n", t.ID, taskID[t.ID])
	}
}

// patchConfig rewrites the two default lane lines to the fixture vocabulary
// (no "waiting" lane). Exact-string replacement on the lines `furrow init`
// ships: if a furrow release changes them, this fails loudly instead of
// leaving a half-patched config.
func patchConfig(lanes []lane) {
	path := filepath.Join(storeDir, "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	var names, terminal []string
	for _, l := range lanes {
		names = append(names, strconv.Quote(l.Name))
		if !l.Next && l.Name != "inbox" && l.Name != "backlog" {
			terminal = append(terminal, strconv.Quote(l.Name))
		}
	}
	replacements := [][2]string{
		{`order = ["inbox", "backlog", "ready", "in-progress", "waiting", "done", "icebox"]`,
			"order = [" + strings.Join(names, ", ") + "]"},
		{`terminal = ["done", "icebox", "waiting"]`,
			"terminal = [" + strings.Join(terminal, ", ") + "]"},
	}
	s := string(raw)
	for _, r := range replacements {
		if !strings.Contains(s, r[0]) {
			fmt.Fprintf(os.Stderr, "error: config.toml is missing the expected default line %q — furrow's defaults changed, update patchConfig\n", r[0])
			os.Exit(1)
		}
		s = strings.Replace(s, r[0], r[1], 1)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func seedEpics(epics []epic) map[string]string {
	id := make(map[string]string, len(epics))
	for _, e := range epics {
		args := []string{"epic", "add", "--json"}
		if e.Goal != "" {
			args = append(args, "--goal", e.Goal)
		}
		for _, l := range e.Labels {
			args = append(args, "-l", l)
		}
		for _, r := range e.Repos {
			args = append(args, "-r", r)
		}
		keys := make([]string, 0, len(e.Meta))
		for k := range e.Meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "--meta", k+"="+e.Meta[k])
		}
		args = append(args, "--", e.Title)
		id[e.ID] = mustID(args...)

		if e.Standing {
			must(furrow("epic", "set", id[e.ID], "--standing=true"))
		}
		if e.Pinned {
			must(furrow("epic", "set", id[e.ID], "--pinned=true"))
		}
	}
	return id
}

// seedTasks creates every task in its lane — except done ones, which start in
// backlog so `furrow done` can stamp Closed properly later.
func seedTasks(tasks []task, epicID map[string]string) map[string]string {
	id := make(map[string]string, len(tasks))
	for _, t := range tasks {
		lane := t.Status
		if lane == "done" {
			lane = "backlog"
		}
		args := []string{"add", "--json", "-s", lane, "-p", strconv.Itoa(t.Priority)}
		if t.Value > 0 {
			args = append(args, "--value", strconv.Itoa(t.Value))
		}
		if t.Effort > 0 {
			args = append(args, "--effort", strconv.Itoa(t.Effort))
		}
		for _, l := range t.Labels {
			args = append(args, "-l", l)
		}
		for _, r := range t.Repos {
			args = append(args, "-r", r)
		}
		if t.Epic != "" {
			mapped, ok := epicID[t.Epic]
			if !ok {
				fmt.Fprintf(os.Stderr, "error: task %s names unknown epic %s\n", t.ID, t.Epic)
				os.Exit(1)
			}
			args = append(args, "-e", mapped)
		}
		if t.Due != nil {
			args = append(args, "--due", t.Due.Format(time.RFC3339))
		}
		for _, c := range t.Checklist {
			args = append(args, "--check", c.Text)
		}
		for _, r := range t.Refs {
			args = append(args, "--ref", r)
		}
		args = append(args, "--", t.Title)
		id[t.ID] = mustID(args...)
	}
	return id
}

var linkRe = regexp.MustCompile(`\[\[([te]-[0-9a-z]+)\]\]`)

// writeBodies replaces the heading furrow seeded with the fixture body,
// [[id]] links rewritten to the new ids. Body files are the user-owned half
// of the store ("the bodies are YOURS — edit them by hand freely"), so a
// direct write is inside furrow's contract.
func writeBodies(tasks []task, taskID, epicID map[string]string) {
	for _, t := range tasks {
		if t.Body == "" {
			continue
		}
		body := linkRe.ReplaceAllStringFunc(t.Body, func(m string) string {
			old := m[2 : len(m)-2]
			if n, ok := taskID[old]; ok {
				return "[[" + n + "]]"
			}
			if n, ok := epicID[old]; ok {
				return "[[" + n + "]]"
			}
			return m
		})
		path := filepath.Join(storeDir, "bodies", taskID[t.ID]+".md")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}
}

func wireDeps(tasks []task, taskID map[string]string) {
	for _, t := range tasks {
		for _, d := range t.Deps {
			dep, ok := taskID[d]
			if !ok {
				fmt.Fprintf(os.Stderr, "error: task %s names unknown dep %s\n", t.ID, d)
				os.Exit(1)
			}
			must(furrow("dep", taskID[t.ID], dep))
		}
	}
}

func checkItems(tasks []task, taskID map[string]string) {
	for _, t := range tasks {
		for i, c := range t.Checklist {
			if c.Done {
				must(furrow("check", taskID[t.ID], strconv.Itoa(i)))
			}
		}
	}
}

// closeDone stamps Closed on the fixture's done tasks, deps-first so no close
// ever races ahead of what it waited on, then re-pins the fixture priority
// (done may have appended them at the lane tail).
func closeDone(tasks []task, taskID map[string]string) {
	done := make(map[string]task)
	for _, t := range tasks {
		if t.Status == "done" {
			done[t.ID] = t
		}
	}
	closed := make(map[string]bool, len(done))
	for len(closed) < len(done) {
		progressed := false
		for _, t := range tasks { // seed order, filtered — deterministic
			d, isDone := done[t.ID]
			if !isDone || closed[t.ID] {
				continue
			}
			ready := true
			for _, dep := range d.Deps {
				if _, depDone := done[dep]; depDone && !closed[dep] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			must(furrow("done", taskID[t.ID]))
			must(furrow("reorder", taskID[t.ID], strconv.Itoa(d.Priority)))
			closed[t.ID] = true
			progressed = true
		}
		if !progressed {
			fmt.Fprintln(os.Stderr, "error: dependency cycle among done tasks")
			os.Exit(1)
		}
	}
}

// finishEpics wires epic deps, closes the fixture's closed boxes, and
// activates the active one — in that order, so a dep on a closed box is
// added while it is still open and then RESOLVES AWAY, arriving in reads
// exactly the way the fixture models it (in deps, absent from open_deps).
func finishEpics(epics []epic, epicID map[string]string) {
	for _, e := range epics {
		for _, d := range e.Deps {
			dep, ok := epicID[d]
			if !ok {
				fmt.Fprintf(os.Stderr, "error: epic %s names unknown dep %s\n", e.ID, d)
				os.Exit(1)
			}
			must(furrow("epic", "dep", epicID[e.ID], "--", dep))
		}
	}
	for _, e := range epics {
		if e.Closed {
			must(furrow("epic", "done", epicID[e.ID]))
		}
	}
	for _, e := range epics {
		if e.Active {
			must(furrow("epic", "activate", epicID[e.ID]))
		}
	}
}

// verify reads the seeded store back through furrow and compares it against
// the seed: per-lane counts, every task's lane/priority/deps under the id
// map, and each epic's active/standing/pinned/progress plus c4mt-style
// open-dep resolution. Seeding that "ran" is not seeding that produced the
// intended board.
func verify(seed seedData, taskID, epicID map[string]string) {
	var rows []struct {
		ID       string   `json:"id"`
		Status   string   `json:"status"`
		Priority int      `json:"priority"`
		Deps     []string `json:"deps"`
		Closed   *string  `json:"closed"`
	}
	if err := json.Unmarshal(must(furrow("ls", "-r", "", "--json")), &rows); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	byID := make(map[string]int, len(rows))
	for i, r := range rows {
		byID[r.ID] = i
	}
	fail := false
	for _, t := range seed.Tasks {
		i, ok := byID[taskID[t.ID]]
		if !ok {
			fmt.Fprintf(os.Stderr, "verify: %s (%s) missing from store\n", t.ID, taskID[t.ID])
			fail = true
			continue
		}
		r := rows[i]
		if r.Status != t.Status || r.Priority != t.Priority {
			fmt.Fprintf(os.Stderr, "verify: %s: lane/priority %s/%d, want %s/%d\n", t.ID, r.Status, r.Priority, t.Status, t.Priority)
			fail = true
		}
		if t.Status == "done" && (r.Closed == nil || *r.Closed == "") {
			fmt.Fprintf(os.Stderr, "verify: %s: done but no closed stamp\n", t.ID)
			fail = true
		}
		want := make([]string, 0, len(t.Deps))
		for _, d := range t.Deps {
			want = append(want, taskID[d])
		}
		sort.Strings(want)
		got := append([]string(nil), r.Deps...)
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			fmt.Fprintf(os.Stderr, "verify: %s: deps %v, want %v\n", t.ID, got, want)
			fail = true
		}
	}
	if len(rows) != len(seed.Tasks) {
		fmt.Fprintf(os.Stderr, "verify: store has %d tasks, seed has %d\n", len(rows), len(seed.Tasks))
		fail = true
	}

	var boxes []struct {
		ID       string   `json:"id"`
		Active   bool     `json:"active"`
		Standing bool     `json:"standing"`
		Pinned   bool     `json:"pinned"`
		OpenDeps []string `json:"open_deps"`
		Progress struct {
			Done  int `json:"done"`
			Total int `json:"total"`
		} `json:"progress"`
	}
	if err := json.Unmarshal(must(furrow("epic", "ls", "-r", "", "--json")), &boxes); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	boxByID := make(map[string]int, len(boxes))
	for i, b := range boxes {
		boxByID[b.ID] = i
	}
	for _, e := range seed.Epics {
		i, ok := boxByID[epicID[e.ID]]
		if !ok {
			if e.Closed {
				continue // closed boxes are absent from the open-only read — as modeled
			}
			fmt.Fprintf(os.Stderr, "verify: epic %s (%s) missing from open read\n", e.ID, epicID[e.ID])
			fail = true
			continue
		}
		b := boxes[i]
		if e.Closed {
			fmt.Fprintf(os.Stderr, "verify: epic %s should be closed but is open\n", e.ID)
			fail = true
		}
		if b.Active != e.Active || b.Standing != e.Standing || b.Pinned != e.Pinned {
			fmt.Fprintf(os.Stderr, "verify: epic %s: active/standing/pinned %v/%v/%v, want %v/%v/%v\n",
				e.ID, b.Active, b.Standing, b.Pinned, e.Active, e.Standing, e.Pinned)
			fail = true
		}
		wantDone, wantTotal := 0, 0
		for _, t := range seed.Tasks {
			if t.Epic == e.ID {
				wantTotal++
				if t.Status == "done" {
					wantDone++
				}
			}
		}
		if b.Progress.Done != wantDone || b.Progress.Total != wantTotal {
			fmt.Fprintf(os.Stderr, "verify: epic %s: progress %d/%d, want %d/%d\n",
				e.ID, b.Progress.Done, b.Progress.Total, wantDone, wantTotal)
			fail = true
		}
		var wantOpen []string
		for _, d := range e.Deps {
			for _, o := range seed.Epics {
				if o.ID == d && !o.Closed {
					wantOpen = append(wantOpen, epicID[d])
				}
			}
		}
		sort.Strings(wantOpen)
		gotOpen := append([]string(nil), b.OpenDeps...)
		sort.Strings(gotOpen)
		if strings.Join(gotOpen, ",") != strings.Join(wantOpen, ",") {
			fmt.Fprintf(os.Stderr, "verify: epic %s: open_deps %v, want %v\n", e.ID, gotOpen, wantOpen)
			fail = true
		}
	}
	if fail {
		fmt.Fprintln(os.Stderr, "verify: FAILED — the store does not match the seed")
		os.Exit(1)
	}
	fmt.Println("verify: store matches seed (lanes, priorities, deps, epic wiring)")
}
