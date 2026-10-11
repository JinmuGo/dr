package agent

import "context"

// Models returns the models the CLI of the agent named name can be asked with, in the
// order the agent lists them.
func Models(ctx context.Context, dirs Dirs, name string) ([]Model, error) {
	a, ok := named(name)
	if !ok {
		return nil, unknown(name)
	}
	return a.models(ctx, dirs)
}
