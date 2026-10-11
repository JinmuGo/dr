package agent

// Fix marks an Edit that dr made, not the agent: a fix the session sent back for a comment
// on its lines, which dr applied. The Agent and Session of such an edit are those of the
// session the comment went to, so a on the fixed lines asks that session.
type Fix struct {
	ID      string // of the record dr wrote when it applied the fix
	Comment string // the text of the comment the fix was for
	Note    string // what the session said with the fix, "" for nothing
	Fork    string // the fork of the session that sent the fix
}
