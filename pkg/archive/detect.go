package archive

import (
	"bytes"
)

type DepDetect struct {
	file string

	ar  *Archiver
	buf *bytes.Buffer
}

func (d *DepDetect) Write(b []byte) (int, error) {
	d.findIn(b)
	return len(b), nil
}

func (d *DepDetect) findIn(src []byte) {
	orig := src

	sp := []byte(d.ar.StorePath)

	if d.buf.Len() > 0 {
		d.buf.Write(src)
		src = d.buf.Bytes()
	}

	for len(src) > 0 {
		idx := bytes.Index(src, sp)
		if idx == -1 {
			break
		}

		// scan the bit right after the store path to find the hash seen
		var hash string

		start := idx + len(sp) + 1

		var j int

		for j = start; j < len(src); j++ {
			_, found := validHashChars[src[j]]
			if !found {
				hash = string(src[start:j])
				break
			}
		}

		// partial
		if j == len(src) {
			break
		}

		d.ar.dependencies[hash] = struct{}{}

		src = src[j:]
	}

	if len(orig) > 50 {
		tail := orig[len(orig)-50:]
		if bytes.IndexByte(tail, d.ar.StorePath[0]) != -1 {
			d.buf.Write(tail)
		} else {
			d.buf.Reset()
		}
	} else {
		d.buf.Reset()
	}
}
