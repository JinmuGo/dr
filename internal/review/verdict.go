package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Verdict is what the reviewer decided about the diff, as :approve and :request say it.
type Verdict string

const (
	VerdictNone    Verdict = "none"    // dr quit without a decision
	VerdictApprove Verdict = "approve" // the changes are accepted as they are
	VerdictRequest Verdict = "request" // the changes need work, as the comments say
)

// Report is what dr --review writes when it quits: the decision and the comments it was
// made with, so the agent that wrote the changes can act on them.
type Report struct {
	Verdict  Verdict         `json:"verdict"`
	Root     string          `json:"root"`  // top directory of the worktree reviewed
	Ref      string          `json:"ref"`   // the ref the worktree was compared with
	Base     string          `json:"base"`  // the commit the diff starts from
	Files    []string        `json:"files"` // the paths that differ from Base, slash-separated
	Comments []ReportComment `json:"comments"`
}

// ReportComment is a comment of the review, on lines of one file.
type ReportComment struct {
	Path string `json:"path"` // slash-separated, relative to Root
	// First and Last are the range in the current file the comment covers, from 1. Both
	// are 0 when the comment is only on deleted lines.
	First int `json:"first"`
	Last  int `json:"last"`
	// Lines are the diff lines the comment is on, each starting with " ", "+" or "-".
	Lines   []string `json:"lines"`
	Text    string   `json:"text"`
	Agent   string   `json:"agent"`   // the agent whose session wrote the lines
	Session string   `json:"session"` // that session
	// Fixed tells a fix dr applied for the comment is in place, so it is already addressed.
	Fixed bool `json:"fixed,omitempty"`
}

// WriteReport writes r as JSON to the file at path, making its directory when it is
// missing. It writes a temporary file beside it and renames that over it, so a reader
// never sees a half-written report.
func WriteReport(path string, r Report) error {
	if r.Files == nil {
		r.Files = []string{}
	}
	if r.Comments == nil {
		r.Comments = []ReportComment{}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return fmt.Errorf("write review report: %w", err)
	}
	return nil
}
