package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/fix"
	"github.com/JinmuGo/dr/internal/git"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
)

// spike is where the sessions in agent/testdata worked. The tests move them to a new repo.
const spike = "/home/dev/Codes/personal/dr-spike2"

// fixClock returns a clock that starts an hour after start, after the session edits of the
// tests, and goes a second ahead each time it is read, so the records it times follow
// each other.
func fixClock() func() time.Time {
	now := start.Add(time.Hour)
	return func() time.Time {
		now = now.Add(time.Second)
		return now
	}
}

// writeTree writes the current lines of files, those with a line diff, under a new
// directory, the worktree fixes are read from and applied to, and returns it.
func writeTree(t *testing.T, files []review.File) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		if f.NoDiff != "" {
			continue
		}
		var b strings.Builder
		for _, l := range f.Lines {
			if l.New > 0 {
				b.WriteString(l.Text + "\n")
			}
		}
		writeFile(t, filepath.Join(root, filepath.FromSlash(f.Path)), b.String())
	}
	return root
}

// fixScreen returns the screen on files in a 125×40 terminal that asks through f, works on
// the worktree root and reads the time from now. It does not tick.
func fixScreen(files []review.File, saved Saved, f *fakeAsk, root string, now func() time.Time) *model {
	m := New(files, nil, saved, Asker{Ask: f.ask, Installed: f.isInstalled, Root: root}).(*model)
	m.tick, m.now = noTick, now
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 40})
	return m
}

// run runs cmd as Bubble Tea does: what it brings goes to m, and so on with the command m
// returns, and each command of a batch runs.
func run(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	default:
		_, next := m.Update(msg)
		run(m, next)
	}
}

// typeAll types keys into m like typeKeys, and runs each command of a batch a key returns.
func typeAll(m *model, keys string) {
	for _, k := range keyPresses(keys) {
		_, cmd := m.Update(k)
		run(m, cmd)
	}
}

// records returns what the log in dir holds.
func records(t *testing.T, dir string) state.Records {
	t.Helper()
	r, bad, err := state.NewLog(filepath.Join(dir, logFile)).Read()
	if err != nil || bad > 0 {
		t.Fatalf("reading the log: %d bad lines, %v", bad, err)
	}
	return r
}

// boxOf returns the text of the box of c, as paragraphText writes it.
func boxOf(m *model, c *comment) string {
	return paragraphText(slices.Concat(m.commentText(c), m.fixOverlay(c)))
}

// commentOf returns the comment of m with text, or nil.
func commentOf(m *model, text string) *comment {
	i := slices.IndexFunc(m.comments, func(c *comment) bool { return c.Text == text })
	if i < 0 {
		return nil
	}
	return m.comments[i]
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// agentRepo returns a new repo whose base commit holds base and whose worktree holds now
// after it, files by their slash-separated paths, and the home directory with the session
// logs of agent/testdata in logs, by where they go under home, moved to the repo.
func agentRepo(t *testing.T, base, now, logs map[string]string) (repo, home string) {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir()) // git reports paths with symlinks resolved
	if err != nil {
		t.Fatal(err)
	}
	repo, home = filepath.Join(tmp, "repo"), filepath.Join(tmp, "home")
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
		// The base commit comes before the sessions.
		"GIT_AUTHOR_DATE": "2026-10-09T12:00:00Z", "GIT_COMMITTER_DATE": "2026-10-09T12:00:00Z",
	} {
		t.Setenv(k, v)
	}
	for path, data := range base {
		writeFile(t, filepath.Join(repo, path), data)
	}
	runGit(t, repo, "init", "-q", "-b", "main")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "base")
	for path, data := range now {
		writeFile(t, filepath.Join(repo, path), data)
	}
	for dest, name := range logs {
		log := readFile(t, filepath.Join("../agent/testdata", name))
		writeFile(t, filepath.Join(home, dest), strings.ReplaceAll(log, spike, repo))
	}
	return repo, home
}

// codexRepo returns a repo where a Codex session wrote the new file codex/ratelimit.go, 71
// lines, and the home directory with its session log. On the screen line n of the file is
// row n+1.
func codexRepo(t *testing.T) (repo, home string) {
	t.Helper()
	return agentRepo(t,
		map[string]string{"README": "dr\n"},
		map[string]string{"codex/ratelimit.go": readFile(t, "../review/testdata/codex-ratelimit.go")},
		map[string]string{".codex/sessions/2026/10/09/rollout-1.jsonl": "codex-01a1209b-a5af-7992-ba5a-c5fdd6a39255.jsonl"})
}

// repoScreen returns the screen on the changed files of repo, with the sessions under home,
// as dr opens it: it keeps its state in the repo's git directory, reads the files again the
// same way and asks through f. It returns the state directory too.
func repoScreen(t *testing.T, repo, home string, f *fakeAsk, now func() time.Time) (*model, string) {
	t.Helper()
	dir, err := git.Path(repo, "dr")
	if err != nil {
		t.Fatal(err)
	}
	read := func() ([]review.File, []agent.Problem, error) {
		r, err := review.Open(repo, home, "")
		if err != nil {
			return nil, nil, err
		}
		paths, err := r.Changed()
		if err != nil {
			return nil, nil, err
		}
		files := make([]review.File, len(paths))
		for i, path := range paths {
			if files[i], err = r.File(path); err != nil {
				return nil, nil, err
			}
		}
		return files, r.Problems, nil
	}
	files, _, err := read()
	if err != nil {
		t.Fatal(err)
	}
	saved := Load(dir)
	saved.Reload = read
	return fixScreen(files, saved, f, repo, now), dir
}

// codexFixes is an answer to a fix request for two comments of codex/ratelimit.go: one on
// line 13, which it turns into two lines, and one on line 50.
var codexFixes = agent.Answer{Fork: "fork-7", Model: "gpt-6-astra", Took: 4 * time.Second, Text: "Here they are.\n" +
	`{"fixes": [{"comment": 1, "new_lines": ["\t// rate is in tokens a second.", "\trate        float64"], "note": "added a comment"},` +
	`{"comment": 2, "new_lines": ["\tif n < 0 || l.burst < n {"], "note": ""}]}`}

func TestSubmit(t *testing.T) {
	// :submit sends one request to each session the pending comments go to, each with its
	// comments numbered from 1 in the order of the diff, to a new fork of the session in its
	// directory and with its model. The deleted line of a comment goes as context. Each
	// comment waits for its fix, and the log keeps each request.
	files, dir := testFiles(), t.TempDir()
	f := &fakeAsk{answer: answer}
	m := fixScreen(files, Load(dir), f, writeTree(t, files), fixClock())
	cmd := typeAndKeep(m, "4jcone<enter>gg11jVjctwo<enter>Gcthree<enter>:submit<enter>")
	if m.message != "Sent 2 fix requests" || len(m.pendingComments()) != 0 || len(m.busy) != 2 {
		t.Fatalf("message %q, %d pending groups, %d busy; want 2 requests sent, nothing pending", m.message, len(m.pendingComments()), len(m.busy))
	}
	for _, c := range m.comments {
		if box := boxOf(m, c); !strings.Contains(box, "To ") || !strings.Contains(box, "  asking… ") {
			t.Errorf("box of %q:\n%s\nwant it asking", c.Text, box)
		}
	}
	run(m, cmd)
	asked := f.asked()
	slices.SortFunc(asked, func(a, b agent.Question) int { return strings.Compare(a.Agent, b.Agent) })
	if len(asked) != 2 {
		t.Fatalf("asked %d sessions, want 2", len(asked))
	}
	claude, omp := asked[0], asked[1]
	if claude.Session != claudeSession || claude.Cwd != "/work" || claude.Model != "claude-opus-5-5" || claude.Fork != "" ||
		!strings.Contains(claude.Prompt, "\n1. a.go:2-2\n>Changed 2\nRequest: one\n2. c/new.go:4-4\n>\tvar x = 1\nRequest: three\n") {
		t.Errorf("claude was asked %+v", claude)
	}
	if omp.Session != ompSession || omp.Log != subagent.Log || omp.Cwd != "/work" || strings.Contains(omp.Prompt, "\n2. ") ||
		!strings.Contains(omp.Prompt, "\n1. a.go:14-14. Lines marked - were deleted.\n>line 14\n-line 15\nRequest: two\n") {
		t.Errorf("omp was asked %+v", omp)
	}
	ids := func(texts ...string) []string {
		var out []string
		for _, text := range texts {
			out = append(out, commentOf(m, text).ID)
		}
		return out
	}
	requests := records(t, dir).Requests
	slices.SortFunc(requests, func(a, b state.Request) int { return strings.Compare(a.Agent, b.Agent) })
	if len(requests) != 2 || requests[0].Session != claudeSession || !slices.Equal(requests[0].Comments, ids("one", "three")) ||
		requests[1].Session != ompSession || !slices.Equal(requests[1].Comments, ids("two")) || requests[0].Relation != "author" {
		t.Errorf("requests %+v", requests)
	}
}

func TestSubmitNothing(t *testing.T) {
	// : opens an input on the status line. With no pending comment :submit says so, a word
	// that is not a dr command says that, and Esc closes the input without running it.
	files := testFiles()
	f := &fakeAsk{answer: answer}
	m := fixScreen(files, Saved{}, f, writeTree(t, files), fixClock())
	typeKeys(m, ":sub")
	if status := statusLine(t, m); !m.commanding || !strings.HasPrefix(status, " COMMAND   :sub") {
		t.Fatalf(": status line %q, want the input", status)
	}
	typeKeys(m, "mit<enter>")
	if m.commanding || m.message != "No comments to submit" {
		t.Errorf(":submit: input open %t, message %q", m.commanding, m.message)
	}
	typeKeys(m, ":ship<enter>")
	if m.message != "Not a dr command: ship" {
		t.Errorf(":ship: message %q", m.message)
	}
	typeKeys(m, "4jcone<enter>:submit<esc>")
	if m.commanding || len(f.asked()) != 0 || len(m.pendingComments()) != 1 {
		t.Errorf("Esc: input open %t, %d asked, want nothing sent", m.commanding, len(f.asked()))
	}
}

func TestSubmitBusy(t *testing.T) {
	// A session answering a question is skipped and named, and the other session's
	// comments go. Once the answer came, its comments go too.
	files := testFiles()
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Text: `{"fixes":[{"comment":1,"new_lines":["x"]}]}`}}
	m := fixScreen(files, Saved{}, f, writeTree(t, files), fixClock())
	typeKeys(m, "4jcone<enter>gg11jVjctwo<enter>")
	question := typeAndKeep(m, "gg4ja<enter>")
	typeKeys(m, "<esc>")
	cmd := typeAndKeep(m, ":submit<enter>")
	if m.message != "Sent 1 fix request. Skipped claude c1a0de00, still answering" {
		t.Errorf("message %q", m.message)
	}
	run(m, cmd)
	if asked := f.asked(); len(asked) != 1 || asked[0].Agent != "omp" || !commentOf(m, "one").pending() {
		t.Fatalf("asked %+v, want omp alone and one still pending", asked)
	}
	run(m, question)
	typeAll(m, ":submit<enter>")
	if asked := f.asked(); len(asked) != 3 || asked[2].Agent != "claude" || !strings.Contains(asked[2].Prompt, "Request: one") {
		t.Errorf("after the answer: asked %+v, want claude asked to fix one", asked)
	}
}

func TestSubmitFixes(t *testing.T) {
	// The fixes that come show under their comments: who sent them with the model and how
	// long it took, the comment's lines deleted and the new lines added, and the note; a fix
	// that keeps the lines has only its note. The comments are no longer pending. The log
	// keeps a fix record for each, with the lines and the file as sent. The status line tells
	// of the fixes while the cursor is elsewhere, and Enter goes to them. On a line of a
	// comment with a fix it shows y and x. The fixes are only drawn: moves, search and the
	// row count do not see them. The fork that answered goes on for the next question.
	files, dir := testFiles(), t.TempDir()
	root := writeTree(t, files)
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Model: "claude-opus-5-5", Took: 7012 * time.Millisecond,
		Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"],"note":"renamed"},{"comment":2,"note":"x is fine"}]}`}}
	m := fixScreen(files, Load(dir), f, root, fixClock())
	typeAll(m, "4jcone<enter>Gcthree<enter>gg:submit<enter>")
	one, three := commentOf(m, "one"), commentOf(m, "three")
	if want := "▍ To claude c1a0de00\n▍ one\n▍\n▍ claude c1a0de00  fix  claude-opus-5-5 · 7s\n▍ -Changed 2\n▍ +Changed two\n▍ renamed"; boxOf(m, one) != want {
		t.Errorf("box of one:\n%s\nwant\n%s", boxOf(m, one), want)
	}
	if want := "▍ To claude c1a0de00\n▍ three\n▍\n▍ claude c1a0de00  keeps the lines  claude-opus-5-5 · 7s\n▍ x is fine"; boxOf(m, three) != want {
		t.Errorf("box of three:\n%s\nwant\n%s", boxOf(m, three), want)
	}
	if len(m.pendingComments()) != 0 {
		t.Errorf("pending %+v, want none", m.pendingComments())
	}
	_, hash, err := fix.Read(root, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	r := records(t, dir)
	if len(r.Requests) != 1 || len(r.Fixes) != 2 {
		t.Fatalf("%d requests and %d fixes in the log, want 1 and 2", len(r.Requests), len(r.Fixes))
	}
	got := r.Fixes[0]
	if got.Request != r.Requests[0].ID || got.Comment != one.ID || got.Text != "one" || got.Agent != "claude" || got.Session != claudeSession ||
		got.Cwd != "/work" || got.Fork != "fork-1" || got.Model != "claude-opus-5-5" || got.TookMS != 7012 || got.Path != "a.go" ||
		got.First != 2 || !slices.Equal(got.Lines, []string{"Changed 2"}) || got.Hash != hash ||
		!slices.Equal(got.New, []string{"Changed two"}) || got.Keep || got.Note != "renamed" || got.Error != "" {
		t.Errorf("fix record %+v", got)
	}
	if kept := r.Fixes[1]; kept.Comment != three.ID || !kept.Keep || kept.Note != "x is fine" || len(kept.New) != 0 {
		t.Errorf("keep record %+v", kept)
	}

	if m.message != "Fixes from claude c1a0de00 on a.go:2 and 1 more. Enter to read" || strings.Contains(keyLine(t, m), "y  apply") ||
		!strings.Contains(statusLine(t, m), "◆ 1 fix ready") { // the other answer keeps its lines
		t.Errorf("status line %q, key row %q, want the fixes told of and counted", statusLine(t, m), keyLine(t, m))
	}
	typeKeys(m, "<enter>")
	if status, keys := statusLine(t, m), keyLine(t, m); m.cursor != 4 || !strings.HasPrefix(keys, "  y  apply    x  drop    a  ask") || !strings.Contains(status, "row 5/27") {
		t.Errorf("Enter: cursor at row %d, status line %q, key row %q; want row 4 with y and x first", m.cursor, status, keys)
	}
	typeKeys(m, "j")
	if m.cursor != 5 || !strings.Contains(statusLine(t, m), "row 6/27") {
		t.Errorf("j: cursor at row %d, want row 5", m.cursor)
	}
	typeKeys(m, "/Changed two<enter>")
	if m.cursor != 5 || !strings.HasPrefix(m.message, "Not found") {
		t.Errorf("search for the fix: cursor at row %d, message %q; want it not found", m.cursor, m.message)
	}
	// The boxes fit any width, in any colors: screen checks each line is as wide as the screen.
	for _, th := range []theme{newTheme(nil, true, false), newTheme(nil, false, false), newTheme(nil, true, true)} {
		m.theme = th
		for _, width := range []int{125, 70, 41, 30, 9} {
			m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
			screen(t, m)
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 40})
	typeKeys(m, "ka<enter>")
	if asked := f.asked(); len(asked) != 2 || asked[1].Fork != "fork-1" {
		t.Errorf("a after the fixes: asked %+v, want the fork that sent them", asked)
	}
}

func TestSubmitError(t *testing.T) {
	// When the session does not answer, or its answer holds no fixes, each comment of the
	// request gets a fix record that says why. The comments are pending again, and their
	// boxes say why.
	files, dir := testFiles(), t.TempDir()
	f := &fakeAsk{err: errors.New("claude exited: rate limited")}
	m := fixScreen(files, Load(dir), f, writeTree(t, files), fixClock())
	typeAll(m, "4jcone<enter>gg:submit<enter>")
	one := commentOf(m, "one")
	if m.message != "No fix from claude c1a0de00 on a.go:2. Enter to read" || len(m.pendingComments()) != 1 {
		t.Errorf("message %q, %d pending groups; want no fix told of and one pending", m.message, len(m.pendingComments()))
	}
	if want := "▍ To claude c1a0de00  pending\n▍ one\n▍\n▍ claude c1a0de00  no fix\n▍ claude exited: rate limited"; boxOf(m, one) != want {
		t.Errorf("box:\n%s\nwant\n%s", boxOf(m, one), want)
	}
	f.err, f.answer = nil, agent.Answer{Fork: "fork-1", Text: "I would rather not change it."}
	typeAll(m, ":submit<enter>")
	fixes := records(t, dir).Fixes
	if len(fixes) != 2 || fixes[0].Error != "claude exited: rate limited" || !strings.HasPrefix(fixes[1].Error, "no JSON object in the answer") ||
		!one.pending() {
		t.Errorf("fix records %+v, pending %t; want both errors and one pending", fixes, one.pending())
	}
}

func TestSubmitEarlier(t *testing.T) {
	// The next request to a session tells which of its earlier fixes were applied and which
	// dropped, and goes on in the fork that sent them.
	files := testFiles()
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Model: "claude-opus-5-5",
		Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"]},{"comment":2,"new_lines":["\tvar x = 2"]}]}`}}
	m := fixScreen(files, Load(t.TempDir()), f, writeTree(t, files), fixClock())
	typeAll(m, "4jcone<enter>Gcthree<enter>:submit<enter>gg4jyGx:submit<enter>")
	asked := f.asked()
	if len(asked) != 2 || asked[1].Fork != "fork-1" || !strings.Contains(asked[1].Prompt,
		"Earlier fixes in this conversation and what the person did with them:\n* a.go:2-2, applied: one\n* c/new.go:4-4, dropped: three\n1. c/new.go:4-4\n") {
		t.Errorf("second request %+v", asked[len(asked)-1])
	}
}

func TestSubmitChanged(t *testing.T) {
	// A comment whose lines are not at its range in the file any more is not sent, and the
	// status line says why; the other comments go.
	files := testFiles()
	root := writeTree(t, files)
	f := &fakeAsk{answer: answer}
	m := fixScreen(files, Saved{}, f, root, fixClock())
	writeFile(t, filepath.Join(root, "a.go"), strings.Replace(readFile(t, filepath.Join(root, "a.go")), "Changed 2", "Changed by hand", 1))
	typeAll(m, "4jcone<enter>Gcthree<enter>:submit<enter>")
	if m.message != "Sent 1 fix request. a.go:2 not sent: its lines changed on disk" {
		t.Errorf("message %q", m.message)
	}
	if asked := f.asked(); len(asked) != 1 || strings.Contains(asked[0].Prompt, "Request: one") || !strings.Contains(asked[0].Prompt, "1. c/new.go:4-4") {
		t.Errorf("asked %+v, want three alone", asked)
	}
}

func TestApplyFix(t *testing.T) {
	// y writes the fix in place of the comment's lines, writes the Applied record and reads
	// the diff again, where the new lines have the fix as their origin and the comment is
	// gone. A second fix to the same file applies after it, where the first moved its lines.
	repo, home := codexRepo(t)
	m, dir := repoScreen(t, repo, home, &fakeAsk{answer: codexFixes}, fixClock())
	typeAll(m, "15Gcdocument rate<enter>52Gcmark the limit<enter>:submit<enter>")
	ids := []string{commentOf(m, "document rate").ID, commentOf(m, "mark the limit").ID}
	path := filepath.Join(repo, "codex", "ratelimit.go")
	before := readFile(t, path)
	_, sent, err := fix.Read(repo, "codex/ratelimit.go")
	if err != nil {
		t.Fatal(err)
	}
	typeAll(m, "15Gy")
	first := strings.Replace(before, "\trate        float64\n", "\t// rate is in tokens a second.\n\trate        float64\n", 1)
	if got := readFile(t, path); got != first {
		t.Fatalf("after y the file is\n%s", got)
	}
	applied := records(t, dir).Applied
	if len(applied) != 1 {
		t.Fatalf("%d applied records, want 1", len(applied))
	}
	a := applied[0]
	if a.Comment != ids[0] || a.Text != "document rate" || a.Note != "added a comment" || a.Agent != "codex" || a.Session == "" ||
		a.Fork != "fork-7" || a.Model != "gpt-6-astra" || a.Effort != "medium" || a.Path != "codex/ratelimit.go" || a.First != 13 ||
		!slices.Equal(a.Old, []string{"\trate        float64"}) ||
		!slices.Equal(a.New, []string{"\t// rate is in tokens a second.", "\trate        float64"}) || a.Before != sent {
		t.Errorf("applied record %+v", a)
	}
	if _, now, err := fix.Read(repo, "codex/ratelimit.go"); err != nil || now != a.After {
		t.Errorf("the file's hash is %s, the record's after %s (%v)", now, a.After, err)
	}
	k := findRow(m, "+\t// rate is in tokens a second.")
	if k < 0 {
		t.Fatal("the diff was not read again")
	}
	if e := m.lines[m.rows[k].right].Edit; e == nil || e.Fix == nil || e.Fix.ID != a.ID || e.Agent != "codex" || e.Session != a.Session {
		t.Errorf("origin of the fixed line %+v, want the fix", e)
	}
	if !slices.Equal(texts(m), []string{"mark the limit"}) || m.message != "Applied the fix for codex/ratelimit.go:13" {
		t.Errorf("comments %q, message %q; want the fixed comment gone and the fix told of", texts(m), m.message)
	}

	row := findRow(m, "+\tif n < 0 || n > l.burst {")
	typeAll(m, strconv.Itoa(row+1)+"Gy")
	second := strings.Replace(first, "\tif n < 0 || n > l.burst {\n", "\tif n < 0 || l.burst < n {\n", 1)
	if got := readFile(t, path); got != second {
		t.Fatalf("after the second y the file is\n%s", got)
	}
	applied = records(t, dir).Applied
	if len(applied) != 2 || applied[1].Comment != ids[1] || applied[1].First != 51 || applied[1].Before != a.After ||
		m.message != "Applied the fix for codex/ratelimit.go:51" || len(m.comments) != 0 {
		t.Errorf("second applied record %+v, message %q, %d comments left", applied[len(applied)-1], m.message, len(m.comments))
	}
}

func TestApplyAdjacentFixes(t *testing.T) {
	// A fix applied to line 13 changes the lines before the comment on line 14. That
	// comment keeps its place under its own line, and y there applies its fix.
	repo, home := codexRepo(t)
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-7", Model: "gpt-6-astra", Text: `{"fixes": [` +
		`{"comment": 1, "new_lines": ["\t// rate is in tokens a second.", "\trate        float64"]},` +
		`{"comment": 2, "new_lines": ["\tburst       int // the most tokens"]}]}`}}
	m, dir := repoScreen(t, repo, home, f, fixClock())
	typeAll(m, "15Gcdocument rate<enter>16Gcdocument burst<enter>:submit<enter>")
	typeAll(m, "15Gy")
	c := commentOf(m, "document burst")
	k := findRow(m, "+\tburst       int")
	if c == nil || k < 0 || c.under != m.rows[k].right || c.fix == nil {
		t.Fatalf("comments %q, burst row %d; want the second comment under its line with its fix", texts(m), k)
	}
	path := filepath.Join(repo, "codex", "ratelimit.go")
	typeAll(m, strconv.Itoa(k+1)+"Gy")
	if got := readFile(t, path); !strings.Contains(got, "\t// rate is in tokens a second.\n\trate        float64\n\tburst       int // the most tokens\n") {
		t.Fatalf("after the second y the file is\n%s", got)
	}
	if applied := records(t, dir).Applied; len(applied) != 2 || applied[1].Comment != c.ID || len(m.comments) != 0 {
		t.Errorf("%d applied records, %d comments left; want both fixes applied", len(applied), len(m.comments))
	}
}

func TestApplyRefusals(t *testing.T) {
	// A line without a fix and a fix that keeps the lines have nothing to apply. When the
	// file changed after the request, y refuses and the fix stays.
	repo, home := codexRepo(t)
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-7", Model: "gpt-6-astra",
		Text: `{"fixes":[{"comment":1,"new_lines":["\trate float64"]},{"comment":2,"note":"it is right"}]}`}}
	m, dir := repoScreen(t, repo, home, f, fixClock())
	typeAll(m, "15Gcone<enter>52Gctwo<enter>:submit<enter>")
	for _, tc := range []struct{ keys, want string }{
		{"3Gy", "No fix on this line"},
		{"52Gy", "Nothing to apply: the session left the lines as they are"},
	} {
		typeAll(m, tc.keys)
		if m.message != tc.want {
			t.Errorf("%q: message %q, want %q", tc.keys, m.message, tc.want)
		}
	}
	path := filepath.Join(repo, "codex", "ratelimit.go")
	changed := readFile(t, path) + "// more\n"
	writeFile(t, path, changed)
	typeAll(m, "15Gy")
	if !strings.Contains(m.message, "the file changed since the fix was requested") || readFile(t, path) != changed ||
		commentOf(m, "one").fix == nil || len(records(t, dir).Applied) != 0 {
		t.Errorf("message %q, fix kept %t; want y refused and the fix kept", m.message, commentOf(m, "one").fix != nil)
	}
}

func TestDropFix(t *testing.T) {
	// x drops the fix: the log keeps a Dropped record, and the comment stays, pending, to be
	// edited and submitted again. Editing a comment drops its fix before the edit is saved.
	files, dir := testFiles(), t.TempDir()
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Model: "claude-opus-5-5",
		Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"]},{"comment":2,"new_lines":["\tvar x = 2"]}]}`}}
	m := fixScreen(files, Load(dir), f, writeTree(t, files), fixClock())
	typeAll(m, "4jcone<enter>Gcthree<enter>:submit<enter>")
	one, three := commentOf(m, "one"), commentOf(m, "three")
	oneFix, threeFix := one.fix.ID, three.fix.ID
	typeKeys(m, "ggx")
	if m.message != "No fix on this line" {
		t.Errorf("x on a header: message %q", m.message)
	}
	typeKeys(m, "4jx")
	if one.fix != nil || !one.pending() || m.message != "Dropped the fix for a.go:2" || !strings.Contains(boxOf(m, one), "  pending") {
		t.Errorf("x: fix %v, pending %t, message %q", one.fix, one.pending(), m.message)
	}
	typeKeys(m, "Gc<ctrl+u>three again<enter>")
	if three.fix != nil || !three.pending() {
		t.Errorf("edited: fix %v, pending %t; want the fix dropped", three.fix, three.pending())
	}
	var dropped []string
	for _, d := range records(t, dir).Dropped {
		dropped = append(dropped, d.Fix)
	}
	if !slices.Equal(dropped, []string{oneFix, threeFix}) {
		t.Errorf("dropped %q, want %q", dropped, []string{oneFix, threeFix})
	}
	// The last drop is the edit's, and the last comment record the edit.
	lines := strings.Split(readFile(t, filepath.Join(dir, logFile)), "\n")
	drop, edit := -1, -1
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, `{"type":"dropped"`):
			drop = i
		case strings.HasPrefix(l, `{"type":"comment"`):
			edit = i
		}
	}
	if drop < 0 || drop > edit {
		t.Errorf("the drop is line %d of the log and the edit line %d; want the drop first", drop, edit)
	}
}

func TestUndoFix(t *testing.T) {
	// :undo puts back the lines of the last fix dr applied, writes the Undone record and
	// reads the diff again; the comment comes back on the lines, pending. With no fix left,
	// it says so.
	repo, home := codexRepo(t)
	m, dir := repoScreen(t, repo, home, &fakeAsk{answer: codexFixes}, fixClock())
	path := filepath.Join(repo, "codex", "ratelimit.go")
	before := readFile(t, path)
	typeAll(m, ":undo<enter>")
	if m.message != "No fix to undo" {
		t.Errorf(":undo with no fix: message %q", m.message)
	}
	typeAll(m, "15Gcdocument rate<enter>52Gcmark the limit<enter>:submit<enter>15Gy:undo<enter>")
	r := records(t, dir)
	if got := readFile(t, path); got != before || len(r.Undone) != 1 || r.Undone[0].Applied != r.Applied[0].ID {
		t.Fatalf("after :undo the file is\n%s\nand the log has %+v", got, r.Undone)
	}
	c := commentOf(m, "document rate")
	if c == nil || !c.pending() || c.under != m.rows[14].right || m.message != "Reverted the fix for codex/ratelimit.go:13" {
		t.Errorf("comment %+v, message %q; want it pending on line 13 again", c, m.message)
	}
	typeAll(m, ":undo<enter>")
	if m.message != "No fix to undo" || readFile(t, path) != before {
		t.Errorf("a second :undo: message %q", m.message)
	}
}

func TestUndoEmpty(t *testing.T) {
	// A fix that leaves no changed file empties the screen, where : still opens, so :undo
	// brings the change and the comment back.
	repo, home := agentRepo(t,
		map[string]string{"omp/notes.md": "a\nold\nc\n"},
		map[string]string{"omp/notes.md": "a\nB\nc\nd\n"},
		map[string]string{".omp/agent/sessions/spike/1.jsonl": "omp-01a1f000-0000-7000-8000-000000000001.jsonl"})
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-9", Text: `{"fixes":[{"comment":1,"new_lines":["old","c"]}]}`}}
	m, _ := repoScreen(t, repo, home, f, fixClock())
	typeAll(m, "4jVjjcput it back<enter>:submit<enter>4jy")
	if len(m.rows) != 0 || m.message != "Applied the fix for omp/notes.md:2-4" {
		t.Fatalf("%d rows, message %q; want no changed file left", len(m.rows), m.message)
	}
	typeAll(m, ":undo<enter>")
	c := commentOf(m, "put it back")
	if readFile(t, filepath.Join(repo, "omp/notes.md")) != "a\nB\nc\nd\n" || findRow(m, "+B") < 0 || c == nil || !c.pending() ||
		m.message != "Reverted the fix for omp/notes.md:2-4" {
		t.Errorf("after :undo: %d rows, comment %+v, message %q", len(m.rows), c, m.message)
	}
}

func TestUndoEmptyNextRun(t *testing.T) {
	// A fix that left no changed file, and dr closed: the next dr opens on the empty screen,
	// where :undo brings the change and the comment back.
	repo, home := agentRepo(t,
		map[string]string{"omp/notes.md": "a\nold\nc\n"},
		map[string]string{"omp/notes.md": "a\nB\nc\nd\n"},
		map[string]string{".omp/agent/sessions/spike/1.jsonl": "omp-01a1f000-0000-7000-8000-000000000001.jsonl"})
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-9", Text: `{"fixes":[{"comment":1,"new_lines":["old","c"]}]}`}}
	now := fixClock()
	first, _ := repoScreen(t, repo, home, f, now)
	typeAll(first, "4jVjjcput it back<enter>:submit<enter>4jy")
	m, _ := repoScreen(t, repo, home, f, now)
	if s := screen(t, m); len(m.files) != 0 || !strings.Contains(s, noChanges) {
		t.Fatalf("%d files on the screen\n%s\nwant none, and %q", len(m.files), s, noChanges)
	}
	typeAll(m, ":undo<enter>")
	c := commentOf(m, "put it back")
	if readFile(t, filepath.Join(repo, "omp/notes.md")) != "a\nB\nc\nd\n" || findRow(m, "+B") < 0 || c == nil || !c.pending() ||
		m.message != "Reverted the fix for omp/notes.md:2-4" {
		t.Errorf("after :undo: %d rows, comment %+v, message %q", len(m.rows), c, m.message)
	}
}

func TestUndoNamesRestoredLines(t *testing.T) {
	// A fix that replaced 3 lines with 1: :undo puts the 3 back, so it names their range.
	repo, home := agentRepo(t,
		map[string]string{"omp/notes.md": "a\nold\nz\n"},
		map[string]string{"omp/notes.md": "a\nB\nc\nd\nz\n"},
		map[string]string{".omp/agent/sessions/spike/1.jsonl": "omp-01a1f000-0000-7000-8000-000000000001.jsonl"})
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-9", Text: `{"fixes":[{"comment":1,"new_lines":["one"]}]}`}}
	m, _ := repoScreen(t, repo, home, f, fixClock())
	typeAll(m, "4jVjjcone line<enter>:submit<enter>Gky")
	if got := readFile(t, filepath.Join(repo, "omp/notes.md")); got != "a\none\nz\n" {
		t.Fatalf("after the fix the file is %q, message %q; want the 3 lines replaced with 1", got, m.message)
	}
	typeAll(m, ":undo<enter>")
	if got := readFile(t, filepath.Join(repo, "omp/notes.md")); got != "a\nB\nc\nd\nz\n" || m.message != "Reverted the fix for omp/notes.md:2-4" {
		t.Errorf("after :undo the file is %q, message %q; want omp/notes.md:2-4", got, m.message)
	}
}

func TestFixLock(t *testing.T) {
	// Without the lock on the worktree, the screen opens and says why, and y and :undo write
	// no file and say why too: another dr holds it, or the lock file cannot be opened.
	for _, tc := range []struct {
		name       string
		lock       func(t *testing.T, dir string) error // takes the lock as dr does, failing
		open, deny string
	}{
		{"another dr", func(t *testing.T, dir string) error {
			unlock, err := state.Lock(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := unlock(); err != nil {
					t.Error(err)
				}
			})
			_, err = state.Lock(dir)
			if !errors.Is(err, state.ErrLocked) {
				t.Skipf("this system takes no lock: %v", err)
			}
			return err
		}, lockedOpen, lockedRefuse},
		{"an unreadable lock file", func(t *testing.T, dir string) error {
			writeFile(t, filepath.Join(dir, "lock"), "")
			if err := os.Chmod(filepath.Join(dir, "lock"), 0); err != nil {
				t.Fatal(err)
			}
			unlock, err := state.Lock(dir)
			if err == nil {
				_ = unlock()
				t.Skip("the lock file opened without permission")
			}
			return err
		}, "Could not lock the worktree: ", "Could not lock the worktree: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, dir := testFiles(), t.TempDir()
			saved := Load(dir)
			saved.Lock = tc.lock(t, dir)
			why := review.Visible(saved.Lock.Error())
			if !errors.Is(saved.Lock, state.ErrLocked) {
				tc.open += why + "; fixes cannot be applied"
				tc.deny += why + "; fixes cannot be applied"
			}
			root := writeTree(t, files)
			f := &fakeAsk{answer: agent.Answer{Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"]}]}`}}
			m := fixScreen(files, saved, f, root, fixClock())
			if m.message != tc.open {
				t.Errorf("status line at open %q, want %q", m.message, tc.open)
			}
			before := readFile(t, filepath.Join(root, "a.go"))
			for _, keys := range []string{"4jcone<enter>:submit<enter>y", ":undo<enter>"} {
				typeAll(m, keys)
				if m.message != tc.deny || readFile(t, filepath.Join(root, "a.go")) != before {
					t.Errorf("%q: message %q, want %q and the file as it was", keys, m.message, tc.deny)
				}
			}
			if commentOf(m, "one").fix == nil || len(records(t, dir).Applied) != 0 {
				t.Error("y without the lock: want the fix kept and nothing applied")
			}
		})
	}
}

func TestReopenFix(t *testing.T) {
	// A fix that was neither applied nor dropped shows again when the screen opens, and y
	// applies it.
	files, dir := testFiles(), t.TempDir()
	root, now := writeTree(t, files), fixClock()
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Model: "claude-opus-5-5", Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"],"note":"renamed"}]}`}}
	first := fixScreen(files, Load(dir), f, root, now)
	typeAll(first, "4jcone<enter>:submit<enter>")
	m := fixScreen(files, Load(dir), f, root, now)
	c := commentOf(m, "one")
	if c == nil || c.fix == nil || !strings.Contains(boxOf(m, c), "▍ -Changed 2\n▍ +Changed two\n▍ renamed") || c.pending() {
		t.Fatalf("reopened: comment %+v, want its fix under it", c)
	}
	typeAll(m, "4jy")
	if got := readFile(t, filepath.Join(root, "a.go")); !strings.HasPrefix(got, "line 1\nChanged two\nline 3\n") || len(records(t, dir).Applied) != 1 {
		t.Errorf("y after reopening: a.go is\n%s", got)
	}
}

func TestReopenResolved(t *testing.T) {
	// A comment a fix resolved stays hidden when the screen opens again, and :undo there
	// brings it back, pending.
	files, dir := testFiles(), t.TempDir()
	root, now := writeTree(t, files), fixClock()
	f := &fakeAsk{answer: agent.Answer{Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"]},{"comment":2,"new_lines":["\tvar x = 2"]}]}`}}
	first := fixScreen(files, Load(dir), f, root, now)
	typeAll(first, "4jcone<enter>Gcthree<enter>:submit<enter>gg4jy")
	m := fixScreen(files, Load(dir), f, root, now)
	if !slices.Equal(texts(m), []string{"three"}) || m.commentsAt(4) != nil || len(m.pendingComments()) != 0 {
		t.Fatalf("reopened: comments %q, want three alone with its fix", texts(m))
	}
	typeAll(m, ":undo<enter>")
	if c := commentOf(m, "one"); c == nil || !c.pending() || m.message != "Reverted the fix for a.go:2" ||
		!strings.HasPrefix(readFile(t, filepath.Join(root, "a.go")), "line 1\nChanged 2\n") {
		t.Errorf(":undo after reopening: comments %q, message %q", texts(m), m.message)
	}
}

func TestReopenUnanswered(t *testing.T) {
	// A request that had no answer when dr quit leaves its comments pending.
	files, dir := testFiles(), t.TempDir()
	root, now := writeTree(t, files), fixClock()
	first := fixScreen(files, Load(dir), &fakeAsk{answer: answer}, root, now)
	_ = typeAndKeep(first, "4jcone<enter>:submit<enter>") // dr quits before the request goes
	m := fixScreen(files, Load(dir), &fakeAsk{answer: answer}, root, now)
	r := records(t, dir)
	if c := commentOf(m, "one"); len(r.Requests) != 1 || len(r.Fixes) != 0 || c == nil || !c.pending() || !strings.Contains(boxOf(m, c), "  pending") {
		t.Errorf("reopened after %d requests and %d fixes: comment %+v, want it pending", len(r.Requests), len(r.Fixes), c)
	}
}
