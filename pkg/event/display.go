package event

import (
	"context"
	"fmt"
)

type Renderer struct {
	l Listener
}

func (r *Renderer) WithContext(ctx context.Context) context.Context {
	r.l.AddHandler(r.handleEvent)
	return SetContext(ctx, &r.l)
}

func (r *Renderer) handleEvent(event Event) {
	switch ev := event.(type) {
	case *DownloadEvent:
		fmt.Printf("Downloading %s...\n", ev.URL)
	case *HashedEvent:
		fmt.Printf("Hashed %s => %s\n", ev.Entity, ev.Hash)
	}
}
