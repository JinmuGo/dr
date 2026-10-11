package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/JinmuGo/dr/internal/diff"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

const (
	// maxColorBytes bounds each side of a file the screen colors by syntax. A bigger file
	// shows without syntax colors, so a huge file does not stall a key press.
	maxColorBytes = 512 << 10
	// maxWordBytes bounds the product of the lengths of two lines the screen compares word
	// by word, which bounds the table the comparison takes.
	maxWordBytes = 1 << 20
)

// piece is a part of a line of code as the screen shows it, with the token type that sets
// its syntax color, and whether the line's word diff marks it as changed.
type piece struct {
	text    string
	kind    chroma.TokenType
	changed bool
}

// color colors the files that have lines on the screen and are not colored yet. A viewed
// file shows only its header, so it waits until it unfolds.
func (m *model) color() {
	for i := m.top; i < min(m.top+m.diffHeight(), len(m.rows)); i++ {
		if f := m.rows[i].file; !m.files[f].colored && !m.files[f].viewed {
			m.colorFile(f)
		}
	}
}

// colorFile splits the lines of file i into pieces, each version of a line on its own: the
// base version for the left of the split view and a deleted line, the current one for the
// rest. The pieces are syntax colored by the lexer for the file name, and marked where a
// deleted line and the added line the split view pairs it with differ, as GitHub marks
// them.
func (m *model) colorFile(i int) {
	f := &m.files[i]
	f.colored = true
	oldTokens, newTokens := lex(f.path, f.diff)
	for k := f.first; k < f.end; k++ {
		l := m.lines[k]
		if l.Old > 0 {
			m.oldPieces[k] = toPieces(lineTokens(oldTokens, l.Old), l.Text)
		}
		if l.New > 0 {
			m.newPieces[k] = toPieces(lineTokens(newTokens, l.New), l.Text)
		}
	}
	for _, r := range m.split[f.splitHead:] {
		if r.file != i {
			break
		}
		if r.isChange() && r.left >= 0 && r.right >= 0 {
			m.oldPieces[r.left], m.newPieces[r.right] = markPair(m.oldPieces[r.left], m.newPieces[r.right])
		}
	}
}

// pieces returns the pieces of line k, of its base version when old is set and of its
// current version otherwise, or the plain text of the line before its file is colored.
func (m *model) pieces(k int, old bool) []piece {
	p := m.newPieces[k]
	if old {
		p = m.oldPieces[k]
	}
	if p == nil {
		p = toPieces(nil, m.lines[k].Text)
	}
	return p
}

// lex returns the tokens of each line of the base and of the current version of the file at
// path, whose diff is lines, or nil for a version it does not color. Each version is lexed
// whole, so a token that spans lines, such as a block comment, colors all of them.
func lex(path string, lines []origin.Line) (oldTokens, newTokens [][]chroma.Token) {
	lexer := lexers.Match(path)
	if lexer == nil {
		return nil, nil
	}
	lexer = chroma.Coalesce(lexer)
	var oldText, newText strings.Builder
	for _, l := range lines {
		if l.Old > 0 {
			oldText.WriteString(l.Text + "\n")
		}
		if l.New > 0 {
			newText.WriteString(l.Text + "\n")
		}
	}
	return tokenize(lexer, oldText.String()), tokenize(lexer, newText.String())
}

// tokenize returns the tokens of each line of text, or nil when text is too big or the
// lexer fails on it.
func tokenize(lexer chroma.Lexer, text string) (lines [][]chroma.Token) {
	if text == "" || len(text) > maxColorBytes {
		return nil
	}
	defer func() {
		if recover() != nil { // a chroma lexer reports some errors by panicking
			lines = nil
		}
	}()
	tokens, err := chroma.Tokenise(lexer, nil, text)
	if err != nil {
		return nil
	}
	return chroma.SplitTokensIntoLines(tokens)
}

// lineTokens returns the tokens of line n, counted from 1, without its newline, or nil when
// there are none.
func lineTokens(lines [][]chroma.Token, n int) []chroma.Token {
	if n > len(lines) {
		return nil
	}
	tokens := lines[n-1]
	if k := len(tokens) - 1; k >= 0 {
		last := tokens[k]
		last.Value = strings.TrimSuffix(last.Value, "\n")
		tokens = append(tokens[:k:k], last)
	}
	return tokens
}

// toPieces returns text as pieces of the screen text, each token one piece. Without tokens,
// or with tokens that do not make up text, text is one piece of plain text.
func toPieces(tokens []chroma.Token, text string) []piece {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(t.Value)
	}
	if b.String() != text {
		return []piece{{text: display(text), kind: chroma.Text}}
	}
	out := make([]piece, 0, len(tokens))
	for _, t := range tokens {
		if t.Value != "" {
			out = append(out, piece{text: display(t.Value), kind: t.Type})
		}
	}
	return out
}

// markPair marks the parts of a deleted line and its added line that differ, when the
// lines are alike enough for the marks to help.
func markPair(a, b []piece) ([]piece, []piece) {
	aText, bText := join(a), join(b)
	if len(aText)*len(bText) > maxWordBytes {
		return a, b
	}
	aParts, bParts := diff.Words(aText, bText)
	if !alike(aText, aParts, bText) {
		return a, b
	}
	return mark(a, aParts), mark(b, bParts)
}

// alike reports whether a line keeps at least half of the shorter of it and the line it
// changed into, not counting spaces, when parts of it changed. Lines that differ more read
// better without word marks.
func alike(a string, parts [][2]int, b string) bool {
	kept := solid(a)
	for _, p := range parts {
		kept -= solid(a[p[0]:p[1]])
	}
	return kept > 0 && 2*kept >= min(solid(a), solid(b))
}

// solid counts the bytes of s that are not spaces.
func solid(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n += utf8.RuneLen(r)
		}
	}
	return n
}

// join returns the text of pieces.
func join(pieces []piece) string {
	var b strings.Builder
	for _, p := range pieces {
		b.WriteString(p.text)
	}
	return b.String()
}

// mark splits pieces at the edges of parts, byte ranges of their text, and marks the
// pieces inside parts as changed.
func mark(pieces []piece, parts [][2]int) []piece {
	out := make([]piece, 0, len(pieces)+2*len(parts))
	at, k := 0, 0
	for _, p := range pieces {
		for p.text != "" {
			for k < len(parts) && parts[k][1] <= at {
				k++
			}
			n := len(p.text)
			changed := false
			if k < len(parts) {
				if at < parts[k][0] {
					n = min(n, parts[k][0]-at)
				} else {
					changed = true
					n = min(n, parts[k][1]-at)
				}
			}
			out = append(out, piece{text: p.text[:n], kind: p.kind, changed: changed})
			p.text = p.text[n:]
			at += n
		}
	}
	return out
}
