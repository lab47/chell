package hashdetect

import (
	"fmt"
	"hash"

	"golang.org/x/crypto/blake2b"
)

func Hasher(algo string) (hash.Hash, error) {
	switch algo {
	case "b2":
		return blake2b.New256(nil)
	default:
		return nil, fmt.Errorf("unknown algo: %s", algo)
	}
}
