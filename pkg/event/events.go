package event

type DownloadEvent struct {
	URL string
}

func (d *DownloadEvent) EventType() string {
	return "download"
}

type HashedEvent struct {
	Entity string
	Hash   string
}

func (h *HashedEvent) EventType() string {
	return "hashed"
}
