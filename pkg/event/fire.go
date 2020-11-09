package event

import "context"

func Fire(ctx context.Context, event Event) {
	l := FromContext(ctx)
	if l == nil {
		return
	}

	l.Fire(event)
}
