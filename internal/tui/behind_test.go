package tui

import (
	"io"
	"strings"
	"testing"

	"github.com/JinmuGo/dr/internal/review"
)

func TestBehind(t *testing.T) {
	// b writes its hint on the cursor's line, takes one read, an arrow's whole sequence,
	// and erases the hint before dr draws again.
	in, out := strings.NewReader("\x1b[A"), &strings.Builder{}
	b := &behind{}
	b.SetStdin(in)
	b.SetStdout(out)
	if err := b.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := out.String(), behindHint+"\r\x1b[2K"; got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
	if in.Len() != 0 {
		t.Errorf("%d bytes of the key left for dr to read", in.Len())
	}

	// Input that ends is an error the status line tells.
	b.SetStdin(strings.NewReader(""))
	if err := b.Run(); err != io.EOF {
		t.Errorf("Run on no input: %v, want EOF", err)
	}

	// b shows the screen under dr on the diff and when no changed file is left.
	for _, files := range [][]review.File{testFiles(), nil} {
		m := pickScreen(files, t.TempDir(), &fakeAsk{answer: answer}, agentModels())
		if _, cmd := m.Update(keyPresses("b")[0]); cmd == nil {
			t.Errorf("%d files: b returned no command", len(files))
		}
	}
}
