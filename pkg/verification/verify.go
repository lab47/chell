package verification

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"

	"github.com/mr-tron/base58"
)

var ErrWrongSignature = errors.New("wrong signature")

type Verifier struct {
}

func (v *Verifier) findSigner(name string) (ed25519.PublicKey, error) {
	if strings.HasPrefix(name, "1:") {
		data, err := base58.Decode(name[2:])
		if err != nil {
			return nil, err
		}

		key := ed25519.PublicKey(data)

		return key, nil
	}

	return nil, fmt.Errorf("unknown signer id scheme: %s", name[:2])
}

func (v *Verifier) Verify(name string, sig, msg []byte) error {
	k, err := v.findSigner(name)
	if err != nil {
		return err
	}

	if !ed25519.Verify(k, msg, sig) {
		return ErrWrongSignature
	}

	return nil
}
