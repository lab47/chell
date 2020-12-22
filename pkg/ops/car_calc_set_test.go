package ops

import (
	"testing"

	"github.com/lab47/chell/pkg/data"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
)

func TestCarCalcSet(t *testing.T) {
	fsum := blake2b.Sum256([]byte("blah"))
	fake := base58.Encode(fsum[:])

	t.Run("computes cars to install first", func(t *testing.T) {
		var sr staticReader
		sr.info = &data.CarInfo{
			ID: "abcdef-fake-1.0",
			Dependencies: []*data.CarDependency{
				{
					ID:     fake + "-blah-1.0",
					Repo:   "qux.com/pkg",
					Signer: "abcdef",
				},
			},
		}

		var sr2 staticReader
		sr2.info = &data.CarInfo{
			ID: fake + "-blah-1.0",
		}

		var cl CarLookup

		cl.overrides = map[string]CarReader{
			"github.com/blah/foo": &sr,
			"qux.com/pkg":         &sr2,
		}

		var ci CarCalcSet

		ci.Lookup = &cl

		toInstall, err := ci.Calculate("github.com/blah/foo", "abcdef-fake-1.0")
		require.NoError(t, err)

		require.Equal(t, 2, len(toInstall))

		assert.Equal(t, "github.com/blah/foo", toInstall[0].Repo)
		assert.Equal(t, "abcdef-fake-1.0", toInstall[0].ID)

		assert.Equal(t, "qux.com/pkg", toInstall[1].Repo)
		assert.Equal(t, fake+"-blah-1.0", toInstall[1].ID)
	})
}
