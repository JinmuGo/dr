package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/alecthomas/chroma/v2"
)

// searchScreen returns a screen of notes.zz, a file no lexer colors, so each line is one
// piece of plain text. Its rows are:
//
//	0  notes.zz
//	1  @@ -1,2 +1,4 @@
//	2  one
//	3  +beta Beta beta
//	4  three
//	5  +alpha beta
func searchScreen() *model {
	lines := origin.Blame("one\nthree\n", "one\nbeta Beta beta\nthree\nalpha beta\n", nil)
	m := newModel([]review.File{{Path: "notes.zz", Status: 'M', Lines: lines}}, nil, Saved{}, Asker{})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	return m
}

func TestSearchMarks(t *testing.T) {
	// Every match is lit, the current one brighter: while the query is typed the first at or
	// after the cursor, then where Enter, n and N put the cursor. The status line counts
	// which match of all the current one is. A lower case query ignores case.
	m := searchScreen()
	th := &m.theme
	added := th.paint(th.code(chroma.Text), th.addbg)
	lit, current := th.match(added, false), th.match(added, true)
	for _, step := range []struct {
		keys, count string
		cursor      int
		row3, row5  []string // what row 3 and row 5 show lit, the current match first when the row has it
		cur3, cur5  bool
	}{
		{keys: "/beta", count: "1 of 4", row3: []string{"beta", "Beta", "beta"}, row5: []string{"beta"}, cur3: true},
		{keys: "<enter>", count: "1 of 4", cursor: 3, row3: []string{"beta", "Beta", "beta"}, row5: []string{"beta"}, cur3: true},
		{keys: "n", count: "4 of 4", cursor: 5, row3: []string{"beta", "Beta", "beta"}, row5: []string{"beta"}, cur5: true},
		{keys: "N", count: "1 of 4", cursor: 3, row3: []string{"beta", "Beta", "beta"}, row5: []string{"beta"}, cur3: true},
		{keys: "j", count: "4 of 4", cursor: 4, row3: []string{"beta", "Beta", "beta"}, row5: []string{"beta"}, cur5: true},
		{keys: "gg/Beta", count: "1 of 1", row3: []string{"Beta"}, cur3: true}, // a capital minds case
	} {
		typeKeys(m, step.keys)
		if status := statusLine(t, m); m.cursor != step.cursor || !strings.Contains(status, "  "+step.count+"   row ") {
			t.Errorf("after %q: cursor at row %d, status line %q; want row %d and %q first at the right", step.keys, m.cursor, status, step.cursor, step.count)
		}
		for _, r := range []struct {
			row  int
			want []string
			cur  bool
		}{{3, step.row3, step.cur3}, {5, step.row5, step.cur5}} {
			raw := m.drawRow(r.row, 80)
			for i, text := range r.want {
				st := lit
				if i == 0 && r.cur {
					st = current
				}
				if !strings.Contains(raw, st.Render(text)) {
					t.Errorf("after %q: row %d does not light %q as a match (current %t): %q", step.keys, r.row, text, i == 0 && r.cur, raw)
				}
			}
			if !r.cur && strings.Contains(raw, current.Render("beta")) || r.want == nil && strings.Contains(raw, lit.Render("beta")) {
				t.Errorf("after %q: row %d lights what it should not: %q", step.keys, r.row, raw)
			}
		}
	}
	// The current match takes the background's color on the bright match color.
	if fg, bg := current.GetForeground(), current.GetBackground(); !sameColor(fg, th.base) || !sameColor(bg, th.matchcur) || !sameColor(lit.GetBackground(), th.matchbg) {
		t.Errorf("current match %v on %v, other matches on %v; want %v on %v and %v", fg, bg, lit.GetBackground(), th.base, th.matchcur, th.matchbg)
	}
	if !strings.Contains(m.View().Content, th.paint(th.pending, th.panel).Bold(true).Render("1 of 1")) {
		t.Error("the count is not bold in the color of warnings")
	}
}

func TestSearchHeaderAndSplit(t *testing.T) {
	// A file header's path is lit too, and in the split view an added line's match is on the
	// right.
	m := searchScreen()
	th := &m.theme
	typeKeys(m, "/notes")
	name := th.onBand(th.strong, th.surface2).Bold(true)
	if raw := m.drawRow(0, 80); !strings.Contains(raw, th.match(name, true).Render("notes")) {
		t.Errorf("the header does not light notes as the current match: %q", raw)
	}
	typeKeys(m, "<esc>s/alpha")
	if !m.splitShown {
		t.Fatal("the split view does not show")
	}
	added := th.paint(th.code(chroma.Text), th.addbg)
	if raw := m.drawRow(5, 140); !strings.Contains(raw, th.match(added, true).Render("alpha")) {
		t.Errorf("split view: row 5 does not light alpha on its right: %q", raw)
	}
}

func TestSearchNotFoundAndEsc(t *testing.T) {
	// A query no row holds is an error and counts nothing. Esc clears the query and its
	// marks, but a count typed before it goes first.
	m := searchScreen()
	th := &m.theme
	typeKeys(m, "/gamma<enter>")
	status := strings.TrimRight(statusLine(t, m), " ")
	if m.tone != toneErr || !strings.HasPrefix(status, " NORMAL   ✗ Not found: gamma") || !strings.Contains(status, "  0 of 0   row ") {
		t.Errorf("not found: tone %d, status line %q; want the error and 0 of 0", m.tone, status)
	}
	if !strings.Contains(m.View().Content, th.paint(th.del, th.panel).Bold(true).Render("✗ ")) {
		t.Error("not found: the icon is not in the color of errors")
	}
	typeKeys(m, "/beta<enter>3<esc>")
	if m.query != "beta" || m.count != 0 {
		t.Errorf("Esc after a count: query %q, count %d; want the query kept and the count cleared", m.query, m.count)
	}
	typeKeys(m, "<esc>")
	added := th.paint(th.code(chroma.Text), th.addbg)
	if raw := m.drawRow(3, 80); m.query != "" || strings.Contains(statusLine(t, m), " of ") || strings.Contains(raw, th.match(added, false).Render("Beta")) {
		t.Errorf("Esc: query %q, status line %q, row 3 %q; want no query, count or marks", m.query, statusLine(t, m), raw)
	}
	// Esc in the search input closes it and keeps the last query.
	typeKeys(m, "/beta<enter>/alp<esc>")
	if m.searching || m.query != "beta" {
		t.Errorf("Esc in the input: searching %t, query %q; want it closed with beta kept", m.searching, m.query)
	}
}

func TestSearchPlain(t *testing.T) {
	// Without colors a match shows in reverse video against its text, the current one bold.
	m := searchScreen()
	m.theme = newTheme(nil, true, true)
	th := &m.theme
	typeKeys(m, "/beta")
	added := th.paint(th.code(chroma.Text), th.addbg)
	lit, current := th.match(added, false), th.match(added, true)
	if !lit.GetReverse() || lit.GetBold() || !current.GetReverse() || !current.GetBold() {
		t.Errorf("without colors: a match reversed %t, bold %t; the current one reversed %t, bold %t", lit.GetReverse(), lit.GetBold(), current.GetReverse(), current.GetBold())
	}
	if raw := m.drawRow(3, 80); !strings.Contains(raw, current.Render("beta")) || !strings.Contains(raw, lit.Render("Beta")) {
		t.Errorf("without colors: row 3 %q does not show the matches", raw)
	}
}

func TestMatchesIn(t *testing.T) {
	for _, tc := range []struct {
		text, query string
		want        [][2]int
	}{
		{"beta Beta beta", "beta", [][2]int{{0, 4}, {5, 9}, {10, 14}}},
		{"beta Beta beta", "Beta", [][2]int{{5, 9}}},
		{"aaaa", "aa", [][2]int{{0, 2}, {2, 4}}}, // not overlapping
		{"한글 HANGUL 한글", "한글", [][2]int{{0, 6}, {14, 20}}},
		{"ÉCOLE école", "école", [][2]int{{0, 6}, {7, 13}}},
		{"none", "x", nil},
	} {
		if got := matchesIn(tc.text, tc.query, foldCase(tc.query)); !slices.Equal(got, tc.want) {
			t.Errorf("matchesIn(%q, %q) = %v, want %v", tc.text, tc.query, got, tc.want)
		}
	}
}
