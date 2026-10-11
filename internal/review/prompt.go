package review

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/JinmuGo/dr/internal/origin"
)

// What dr adds to a question so that answers stay short and start with a tag that says
// where their reason comes from, by how the session asked relates to the lines: a line
// before where the lines are, which the author does not get, and the end of the question.
const (
	questionEnd = " Start your answer with exactly one tag: [decided] if the reason was agreed with the user in this conversation, [chose] if you chose it yourself, or [guess] if you do not remember or cannot know. Then give the reason in one sentence of at most 25 words, in the language of this conversation, unless the question asks for more. No preamble, no restating the question or the code, no suggestions."
	parentIntro = "A subagent you started wrote these lines. dr cannot ask the subagent itself."
	parentEnd   = " Start your answer with exactly one tag: [decided], [chose] or [guess], for whether the reason for these lines was agreed with the user, chosen by you, or a guess. Then say what you asked the subagent to do and why the lines look this way, in one sentence of at most 25 words, in the language of this conversation, unless the question asks for more. No preamble, no restating the question or the code, no suggestions."
	editorIntro = "dr found no record of who wrote these lines. You edited this file in this conversation, but no file edit of yours wrote these lines; a shell command, another tool or a person may have changed them."
	editorEnd   = " If you changed these lines, start with [decided] or [chose] and say how and why. If not, start with [guess], say they are not yours, and give your best guess. Use one sentence of at most 25 words, in the language of this conversation, unless the question asks for more. No preamble, no restating the question or the code, no suggestions."
	newIntro    = "You did not write these lines and have no conversation about them. Read the file if you need to."
	newEnd      = " Start your answer with [guess]. Then give your best guess from reading the code in one sentence of at most 25 words, in the language of the question, unless the question asks for more. No preamble, no restating the question or the code, no suggestions."
)

// The kinds of answer, by the tag the question asks the answer to start with: its reason
// was agreed with the user, the session asked chose it, or it is a guess.
const (
	Decided = "decided"
	Chose   = "chose"
	Guess   = "guess"
)

// answerTags are the tags an answer can start or end with, and the kind of answer each
// tells: [decided], [chose] and [guess], also wrapped in ** or backticks.
var answerTags = [...]struct{ tag, kind string }{
	{"[decided]", Decided}, {"**[decided]**", Decided}, {"`[decided]`", Decided},
	{"[chose]", Chose}, {"**[chose]**", Chose}, {"`[chose]`", Chose},
	{"[guess]", Guess}, {"**[guess]**", Guess}, {"`[guess]`", Guess},
}

// Prompt returns what dr sends to ask question about lines of the file at path a session of
// relation r to them: a line that tells the session how it relates to the lines unless it
// wrote them, where the lines are, each line quoted, > for a line of the current file and -
// for a deleted one, and the question with what dr adds to it.
func Prompt(r Relation, path string, lines []origin.Line, question string) string {
	var first, last, firstOld, lastOld int
	for _, l := range lines {
		switch {
		case l.New > 0 && first == 0:
			first, last = l.New, l.New
		case l.New > 0:
			last = l.New
		case firstOld == 0:
			firstOld, lastOld = l.Old, l.Old
		default:
			lastOld = l.Old
		}
	}
	var b strings.Builder
	b.WriteString("[dr] Review question. Do not change any code, just answer.\n")
	intro, end := framing(r)
	if intro != "" {
		b.WriteString(intro + "\n")
	}
	switch {
	case first > 0:
		b.WriteString(path + ":" + lineRange(first, last))
	case firstOld == lastOld:
		b.WriteString(path + ", deleted line " + strconv.Itoa(firstOld) + " of the base file")
	default:
		b.WriteString(path + ", deleted lines " + lineRange(firstOld, lastOld) + " of the base file")
	}
	if firstOld > 0 {
		b.WriteString(". Lines marked - were deleted.")
	}
	b.WriteString("\n")
	for _, l := range lines {
		mark := ">"
		if l.New == 0 {
			mark = "-"
		}
		b.WriteString(mark + l.Text + "\n")
	}
	b.WriteString(question + end)
	return b.String()
}

// framing returns what dr adds to a question for a session of relation r to the lines: the
// line before where the lines are, "" for none, and the end of the question.
func framing(r Relation) (intro, end string) {
	switch r {
	case Parent:
		return parentIntro, parentEnd
	case Editor:
		return editorIntro, editorEnd
	case NewSession:
		return newIntro, newEnd
	}
	return "", questionEnd
}

// AnswerKind returns the kind of answer the tag in text tells, "" when it has none, and text
// without the tag, as the origin box shows it. The tag is looked for at the start of text
// after its leading space, and else at its end, in any case.
func AnswerKind(text string) (kind, rest string) {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	for _, t := range answerTags {
		if len(text) >= len(t.tag) && strings.EqualFold(text[:len(t.tag)], t.tag) {
			return t.kind, strings.TrimLeftFunc(text[len(t.tag):], unicode.IsSpace)
		}
	}
	end := strings.TrimRightFunc(text, unicode.IsSpace)
	for _, t := range answerTags {
		if n := len(end) - len(t.tag); n >= 0 && strings.EqualFold(end[n:], t.tag) {
			return t.kind, strings.TrimRightFunc(end[:n], unicode.IsSpace)
		}
	}
	return "", text
}

// lineRange returns the lines from first to last as "14-16", or "14" for one line.
func lineRange(first, last int) string {
	if first == last {
		return strconv.Itoa(first)
	}
	return strconv.Itoa(first) + "-" + strconv.Itoa(last)
}
