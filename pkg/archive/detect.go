package archive

import (
	"bytes"
)

type DepDetect struct {
	file   string
	prefix []byte

	ar  *Archiver
	buf *bytes.Buffer

	state        int
	restOfPrefix []byte
	hashParts    []byte
}

const (
	detectStart = iota
	detectPrefix
	detectHash
)

func (d *DepDetect) Write(b []byte) (int, error) {
	d.findIn(b)
	return len(b), nil
}

func (d *DepDetect) findIn(src []byte) {
	prefix := d.prefix

	if d.state == detectPrefix {
		prefix = d.restOfPrefix
	}

	for _, b := range src {
		switch d.state {
		case detectStart:
			if prefix[0] == b {
				prefix = prefix[1:]
				d.buf.WriteByte(b)

				d.state = detectPrefix
			}
		case detectPrefix:
			if prefix[0] == b {
				d.buf.WriteByte(b)
				prefix = prefix[1:]

				if len(prefix) == 0 {
					d.state = detectHash
					d.hashParts = nil
					d.buf.Reset()
				}
			} else {
				prefix = d.prefix
				d.state = detectStart
				d.buf.Reset()
			}
		case detectHash:
			_, found := validHashChars[b]
			if found {
				d.buf.WriteByte(b)
				d.hashParts = append(d.hashParts, b)
			} else {
				hash := string(d.hashParts)
				d.ar.dependencies[hash] = struct{}{}

				d.state = detectPrefix
				prefix = d.prefix
			}
		}
	}

	d.restOfPrefix = prefix
}
