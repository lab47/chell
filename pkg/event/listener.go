package event

type Event interface {
	EventType() string
}

type Handler func(event Event)

type Listener struct {
	handlers []Handler
}

func (l *Listener) Fire(event Event) {
	for _, h := range l.handlers {
		h(event)
	}
}

func (l *Listener) AddHandler(h Handler) {
	l.handlers = append(l.handlers, h)
}
