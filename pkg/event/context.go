package event

import "context"

type contextType int

var contextKey contextType

func FromContext(ctx context.Context) *Listener {
	l, ok := ctx.Value(contextKey).(*Listener)

	if !ok {
		return nil
	}

	return l
}

func SetContext(ctx context.Context, l *Listener) context.Context {
	return context.WithValue(ctx, contextKey, l)
}
