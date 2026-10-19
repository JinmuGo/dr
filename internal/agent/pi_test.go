package agent

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

// piSession is a Pi 1.3.0 session that wrote a rate limiter and then renamed a field in a
// second request. Its first edit call failed.
const piSession = "01a152c8-00fb-74d4-9060-82bb53bae8df"

func TestLoadPi(t *testing.T) {
	session := fixture(t, "pi-"+piSession+".jsonl")
	// A fork names the session it came from, which did not start it.
	forked := strings.Replace(session, `"cwd":"`+spike+`/pi"}`,
		`"cwd":"`+spike+`/pi","parentSession":"/home/dev/.pi/agent/sessions/--x--/2026-10-19T06-00-00-000Z_01a152c0.jsonl"}`, 1)
	for _, tc := range []struct{ name, log string }{{"a session", session}, {"a fork", forked}} {
		t.Run(tc.name, func(t *testing.T) {
			home := fakeHome(t, map[string]string{
				".pi/agent/sessions/--home-dev-Codes-personal-dr-spike2-pi--/2026-10-19T06-10-01-596Z_" + piSession + ".jsonl": tc.log,
			})
			var loader Loader
			edits, problems := loader.Load(home, since, []string{spike})
			if len(problems) > 0 {
				t.Fatalf("problems: %+v", problems)
			}
			var got []string
			file := ""
			for _, e := range edits {
				got = append(got, fmt.Sprintf("%s %s parent %q %s #%d %.20q %s %s", e.Agent, e.Session, e.Parent,
					strings.TrimPrefix(e.Path, spike+"/"), e.Turn, e.Prompt, e.Model, e.Effort))
				after, ok := e.Apply(file)
				if !ok {
					t.Fatalf("edit #%d does not apply to %q", e.Turn, file)
				}
				file = after
			}
			want := []string{
				`pi ` + piSession + ` parent "" pi/ratelimit.go #1 "Create ratelimit.go:" openai/gpt-6-astra medium`,
				`pi ` + piSession + ` parent "" pi/ratelimit.go #2 "In ratelimit.go, ren" openai/gpt-6-astra medium`,
			}
			if !slices.Equal(got, want) {
				t.Errorf("got %q, want %q", got, want)
			}
			for _, w := range []string{"\tleft int\n", "\tl.left--\n"} {
				if !strings.Contains(file, w) || strings.Contains(file, "tokens") {
					t.Errorf("file after the edits:\n%s\nwant it to contain %q and no tokens", file, w)
				}
			}
		})
	}
}

func TestLoadPiOMPImport(t *testing.T) {
	// Pi holds the OMP sessions it imported as OMP wrote them, under ids of its own. They are
	// Pi's sessions now: pi forks them. A subagent's log names its parent's log in Pi. Each
	// edit replays once, when the OMP logs are still there too and when a Pi session forked
	// the import later.
	const root, child = "omp-01a1f000-0000-7000-8000-000000000001-0a0a0a0a", "omp-01a1f000-0000-7000-8000-000000000002-0b0b0b0b"
	dir := ".pi/agent/sessions/--home-dev-Codes-personal-dr-spike2-omp--/"
	imported := map[string]string{
		dir + "2026-10-09T12-30-00-000Z_" + root + ".jsonl":                  fixture(t, "pi-"+root+".jsonl"),
		dir + "2026-10-09T12-30-00-000Z_" + root + "/0-Task.jsonl":           fixture(t, "pi-"+child+".jsonl"),
		".omp/agent/sessions/elsewhere/2026-10-09T12-00-00-000Z_other.jsonl": `{"type":"session","id":"other","cwd":"/elsewhere"}`,
	}
	withOMP := maps.Clone(imported)
	withOMP[".omp/agent/sessions/spike/01a1f000-1.jsonl"] = fixture(t, "omp-01a1f000-0000-7000-8000-000000000001.jsonl")
	withOMP[".omp/agent/sessions/spike/01a1f000-1/0-Task.jsonl"] = fixture(t, "omp-01a1f000-0000-7000-8000-000000000002.jsonl")
	forked := maps.Clone(imported)
	lines := strings.SplitN(fixture(t, "pi-"+root+".jsonl"), "\n", 2)
	forked[dir+"2026-10-19T09-00-00-000Z_01a1f000-0000-7000-8000-00000000000f.jsonl"] = `{"type":"session","version":3,"id":"01a1f000-0000-7000-8000-00000000000f",` +
		`"timestamp":"2026-10-19T09:00:00.000Z","cwd":"` + spike + `/omp","parentSession":"/home/dev/` + dir + `2026-10-09T12-30-00-000Z_` + root + `.jsonl"}` + "\n" + lines[1]
	for _, tc := range []struct {
		name string
		logs map[string]string
	}{{"imported", imported}, {"the OMP logs there too", withOMP}, {"a fork of the import", forked}} {
		t.Run(tc.name, func(t *testing.T) {
			var loader Loader
			edits, problems := loader.Load(fakeHome(t, tc.logs), since, []string{spike})
			if len(problems) > 0 {
				t.Fatalf("problems: %+v", problems)
			}
			files := map[string]string{spike + "/omp/other.md": "x\n"}
			var got []string
			for _, e := range edits {
				got = append(got, fmt.Sprintf("%s %s parent %q %s %s", e.Agent, e.Session, e.Parent, strings.TrimPrefix(e.Path, spike+"/"), e.Model))
				before := files[e.Path]
				if e.Before != nil {
					before = *e.Before
				}
				after, ok := e.Apply(before)
				if !ok {
					t.Fatalf("edit of %s does not apply to %q", e.Path, before)
				}
				files[e.Path] = after
			}
			want := []string{
				`pi ` + root + ` parent "" omp/notes.md anthropic/claude-opus-5-5`,
				`pi ` + root + ` parent "" omp/notes.md anthropic/claude-opus-5-5`,
				`pi ` + root + ` parent "" omp/notes.md anthropic/claude-opus-5-5`,
				`pi ` + root + ` parent "" omp/other.md anthropic/claude-opus-5-5`,
				`pi ` + child + ` parent "` + root + `" omp/other.md `,
			}
			if !slices.Equal(got, want) {
				t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			if got, want := files[spike+"/omp/notes.md"], "a\nB\nc\nd\n"; got != want {
				t.Errorf("notes.md: got %q, want %q", got, want)
			}
			if got, want := files[spike+"/omp/other.md"], "y\nz\n"; got != want {
				t.Errorf("other.md: got %q, want %q", got, want)
			}
		})
	}
}

func TestPiCodemode(t *testing.T) {
	// A codemode call logs only the calls its code made, with their arguments cut short, so
	// a write it made is an edit dr cannot read, of the file the arguments name, however the
	// code called it and even when the code failed after it. A failed edit and a read change
	// nothing. A call whose code edits but whose result lists no calls names no file, so the
	// log has a Problem.
	home := fakeHome(t, map[string]string{
		".pi/agent/sessions/--home-dev-Codes-personal-dr-spike2-pi--/2026-10-09T14-00-00-000Z_01a1f000-0000-7000-8000-000000000004.jsonl": fixture(t, "pi-01a1f000-0000-7000-8000-000000000004.jsonl"),
	})
	var loader Loader
	edits, problems := loader.Load(home, since, []string{spike})
	var got []string
	for _, e := range edits {
		got = append(got, fmt.Sprintf("%s %s #%d %q %s", e.Unread, strings.TrimPrefix(e.Path, spike+"/"), e.Turn, e.Note, e.Time.Format(time.TimeOnly)))
		if _, ok := e.Apply(""); ok {
			t.Errorf("unread edit of %s fits a file", e.Path)
		}
	}
	want := []string{
		`codemode pi/report.html #1 "올리고 저장할게요." 14:00:05`,
		`codemode pi/aliased.md #1 "" 14:00:09`,
		`codemode pi/half.md #1 "" 14:00:09`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(problems) != 1 || problems[0].Agent != "pi" || problems[0].Unread != 1 || problems[0].Bad != 0 {
		t.Errorf("problems: got %+v, want one of pi with 1 unread edit", problems)
	}
}
