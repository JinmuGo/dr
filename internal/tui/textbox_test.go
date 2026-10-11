package tui

import (
	"slices"
	"strings"
	"testing"
)

// boxAt returns a box holding s without its |, with the cursor where the | was.
func boxAt(s string) textBox {
	at := strings.Index(s, "|")
	return textBox{text: s[:at] + s[at+1:], cursor: at}
}

// marked returns the text of b with a | at the cursor.
func marked(b textBox) string {
	return b.text[:b.cursor] + "|" + b.text[b.cursor:]
}

// typeInto types keys into b, written as for keyPresses, as the input box does with its
// text wrapped at width cells.
func typeInto(b *textBox, keys string, width int) {
	for _, k := range keyPresses(keys) {
		b.edit(k.String(), k.Text, width)
	}
}

func TestTextBoxKeys(t *testing.T) {
	// The cursor is shown as |. Width 20 wraps none of these texts.
	for _, tc := range []struct {
		name, text, keys, want string
	}{
		{"typing at the cursor", "a|c", "b", "ab|c"},
		{"an input method's characters whole", "a|", "왜요", "a왜요|"},
		{"a new line from Shift+Enter", "ab|cd", "<shift+enter>", "ab\n|cd"},
		{"a new line from Alt+Enter", "ab|cd", "<alt+enter>", "ab\n|cd"},
		{"a new line from Ctrl+J", "ab|cd", "<ctrl+j>", "ab\n|cd"},
		{"Backspace across a line break", "ab\n|cd", "<bs>", "ab|cd"},
		{"Delete across a line break", "ab|\ncd", "<delete>", "ab|cd"},
		{"Backspace at the start", "|ab", "<bs>", "|ab"},
		{"Delete at the end", "ab|", "<delete>", "ab|"},
		{"Backspace a wide character whole", "가나|", "<bs>", "가|"},
		{"Backspace a cluster whole", "a👍🏽|", "<bs>", "a|"},
		{"Left over a cluster", "ae\u0301|x", "<left>", "a|e\u0301x"},
		{"Left over a line break", "ab\n|cd", "<left>", "ab|\ncd"},
		{"Right over a wide character", "|가나", "<right>", "가|나"},
		{"Right at the end", "ab|", "<right>", "ab|"},
		{"Ctrl+A", "ab\ncd|ef\ngh", "<ctrl+a>", "ab\n|cdef\ngh"},
		{"Ctrl+E", "ab\ncd|ef\ngh", "<ctrl+e>", "ab\ncdef|\ngh"},
		{"Home", "ab\ncd|ef\ngh", "<home>", "ab\n|cdef\ngh"},
		{"End", "ab\ncd|ef\ngh", "<end>", "ab\ncdef|\ngh"},
		{"Alt+Left to the start of the word", "foo bar  baz|", "<alt+left>", "foo bar  |baz"},
		{"Ctrl+Left past the space", "foo bar  |baz", "<ctrl+left>", "foo |bar  baz"},
		{"Alt+B", "foo bar|", "<alt+b>", "foo |bar"},
		{"a word left across a line break", "foo\n|bar", "<alt+left>", "|foo\nbar"},
		{"a word left in white space only", "  |x", "<alt+left>", "|  x"},
		{"Alt+Right to the next word", "|foo bar", "<alt+right>", "foo |bar"},
		{"Ctrl+Right from white space", "foo|  bar", "<ctrl+right>", "foo  |bar"},
		{"Alt+F at the last word", "foo |bar", "<alt+f>", "foo bar|"},
		{"a word right across a line break", "fo|o\nbar", "<alt+right>", "foo\n|bar"},
		{"Ctrl+U to the start of the line", "ab\ncd|ef", "<ctrl+u>", "ab\n|ef"},
		{"Ctrl+U at the start of a line", "ab\n|cd", "<ctrl+u>", "ab\n|cd"},
		{"Ctrl+W the word before", "foo bar  |baz", "<ctrl+w>", "foo |baz"},
		{"Ctrl+W at the end of a word", "foo bar|", "<ctrl+w>", "foo |"},
		{"Ctrl+W a wide word", "말 한글|", "<ctrl+w>", "말 |"},
		{"a key without text types nothing", "a|", "<tab>", "a|"},
	} {
		b := boxAt(tc.text)
		typeInto(&b, tc.keys, 20)
		if got := marked(b); got != tc.want {
			t.Errorf("%s: %q after %q is %q, want %q", tc.name, tc.text, tc.keys, got, tc.want)
		}
	}
}

func TestTextBoxRows(t *testing.T) {
	// Each line is a row, wrapped by display width before the character that would go past
	// it. A character wider than the box has a row of its own; an empty line is a row too.
	for _, tc := range []struct {
		text  string
		width int
		want  []string
	}{
		{"", 4, []string{""}},
		{"abcd", 4, []string{"abcd"}},
		{"abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"가나다라마", 5, []string{"가나", "다라", "마"}},
		{"a가", 2, []string{"a", "가"}},
		{"가", 1, []string{"가"}},
		{"ab\n\ncd\n", 4, []string{"ab", "", "cd", ""}},
		{"a\tb", 4, []string{"a", "\t", "b"}}, // a tab shows as four spaces
	} {
		b := newTextBox(tc.text)
		var got []string
		for _, r := range b.rows(tc.width) {
			got = append(got, b.text[r.start:r.end])
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q at %d cells: rows %q, want %q", tc.text, tc.width, got, tc.want)
		}
	}
}

func TestTextBoxUpDown(t *testing.T) {
	// Up and Down go by the rows the box shows, to the cell they started at, or the nearest
	// before it on a shorter row, which a run of them keeps. The end of a row that wraps is
	// the start of the next, so the cursor does not stop there. They stay on the first and
	// the last row.
	for _, tc := range []struct {
		name, text string
		width      int
		keys, want string
	}{
		{"up a wrapped row", "abcdef|ghij", 4, "<up>", "ab|cdefghij"},
		{"down a wrapped row", "ab|cdefghij", 4, "<down>", "abcdef|ghij"},
		{"down to the end of the last row", "abcdef|ghij", 4, "<down>", "abcdefghij|"},
		{"up from the start of a wrapped row", "abcd|efgh", 4, "<up>", "|abcdefgh"},
		{"up to a row that wraps", "abcdefgh|\nab", 4, "<up>", "abc|defgh\nab"},
		{"wide characters", "가나다|라", 4, "<up>", "가|나다라"},
		{"a shorter line, then back", "abcde|f\nab\nabcdef", 20, "<down>", "abcdef\nab|\nabcdef"},
		{"the cell kept", "abcde|f\nab\nabcdef", 20, "<down><down>", "abcdef\nab\nabcde|f"},
		{"through an empty line", "ab|c\n\nabc", 20, "<down><down>", "abc\n\nab|c"},
		{"up on the first row", "ab|cd", 20, "<up>", "ab|cd"},
		{"down on the last row", "ab\ncd|", 20, "<down>", "ab\ncd|"},
		{"another key forgets the cell", "abcde|f\nab\nabcdef", 20, "<down><left><down>", "abcdef\nab\na|bcdef"},
	} {
		b := boxAt(tc.text)
		typeInto(&b, tc.keys, tc.width)
		if got := marked(b); got != tc.want {
			t.Errorf("%s: %q after %q is %q, want %q", tc.name, tc.text, tc.keys, got, tc.want)
		}
	}
}

func TestTextBoxPaste(t *testing.T) {
	// A paste goes in at the cursor: each line break as "\n", a tab as four spaces, and
	// other control characters left out.
	b := boxAt("<|>")
	b.paste("one\r\ntwo\rthree\tfour\x1b[31m\x00")
	if got, want := marked(b), "<one\ntwo\nthree    four[31m|>"; got != want {
		t.Errorf("after the paste: %q, want %q", got, want)
	}
}

func TestTextBoxScroll(t *testing.T) {
	// The box shows at most height rows. It scrolls as little as it takes to keep the
	// cursor's row in view, and leaves no empty rows at its bottom.
	b := newTextBox("1\n2\n3\n4\n5\n6")
	shownRows := func() string {
		shown, row, _ := b.layout(10, 3)
		var texts []string
		for _, r := range shown {
			texts = append(texts, b.text[r.start:r.end])
		}
		return strings.Join(texts, " ") + " at " + strings.Repeat("^", row+1)
	}
	for _, step := range []struct{ keys, want string }{
		{"", "4 5 6 at ^^^"},
		{"<up><up>", "4 5 6 at ^"},
		{"<up>", "3 4 5 at ^"},
		{"<down><down><down>", "4 5 6 at ^^^"},
	} {
		typeInto(&b, step.keys, 10)
		b.follow(10, 3)
		if got := shownRows(); got != step.want {
			t.Errorf("after %q: rows %q, want %q", step.keys, got, step.want)
		}
	}
	typeInto(&b, "<ctrl+a>"+strings.Repeat("<bs>", 6), 10) // the text now has no more rows than fit
	b.follow(10, 3)
	if got := shownRows(); got != "1 2 6 at ^^^" {
		t.Errorf("after deleting lines: rows %q, want all three", got)
	}
}
