package agent

// Model is a model the CLI of an agent can be asked with.
type Model struct {
	ID   string // what the CLI takes as its model, such as "sonnet", "gpt-6-luna" or "anthropic/claude-opus-5-5"
	Name string // how the agent names it, "" when it gives no name
}
