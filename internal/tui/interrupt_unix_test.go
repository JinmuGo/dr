//go:build unix

package tui

import (
	"os/exec"
	"strings"
	"testing"
)

func TestInterruptedEnd(t *testing.T) {
	// Leaving a CLI with ctrl+c kills it with SIGINT or makes it exit with 130; neither is
	// an error worth a warning, and the diff rebuilds as usual. Other failures still warn.
	tests := []struct {
		name   string
		script string
		warn   bool
	}{
		{name: "killed by SIGINT", script: "kill -INT $$", warn: false},
		{name: "exit 130", script: "exit 130", warn: false},
		{name: "exit 1", script: "exit 1", warn: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := exec.Command("sh", "-c", tc.script).Run()
			if err == nil {
				t.Fatalf("sh -c %q ended without an error", tc.script)
			}
			m := editScreen(testFiles(), Saved{Reload: reloading(editedFiles())}, &fakeEditor{})
			send(m, editedMsg{err: err, failed: "codex exited"})
			warned := strings.Contains(m.message, "codex exited")
			rebuilt := findRow(m, "+Line 5 edited") >= 0
			if warned != tc.warn || !rebuilt {
				t.Errorf("sh -c %q: message %q, rebuilt %t; want a warning %t and the diff rebuilt", tc.script, m.message, rebuilt, tc.warn)
			}
		})
	}
}
