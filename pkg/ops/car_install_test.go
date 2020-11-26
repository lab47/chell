package ops

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/lab47/chell/pkg/archive"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
)

func TestCarInstall(t *testing.T) {
	topdir, err := ioutil.TempDir("", "carpack")
	require.NoError(t, err)

	defer os.RemoveAll(topdir)

	dir := filepath.Join(topdir, "t")

	fsum := blake2b.Sum256([]byte("blah"))
	fake := base58.Encode(fsum[:])

	testBin := []byte(fmt.Sprintf("#!/bin/sh\ncat %s/%s-blah-1.0/whatever\n", dir, fake))
	echoBin := []byte("#!/bin/sh\necho 'hello'\n")

	t.Run("looks up and installs a car", func(t *testing.T) {
		require.NoError(t, os.Mkdir(dir, 0755))
		defer os.RemoveAll(dir)

		require.NoError(t, os.Mkdir(filepath.Join(dir, "bin"), 0755))

		err := ioutil.WriteFile(filepath.Join(dir, "bin/test"), echoBin, 0644)
		require.NoError(t, err)

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var (
			cp    CarPack
			cinfo archive.CarInfo
		)

		cp.PrivateKey = priv
		cp.PublicKey = pub

		dh, _ := blake2b.New256(nil)

		var sr staticReader

		cinfo.ID = "abcdef-fake-1.0"

		err = cp.Pack(&cinfo, dir, io.MultiWriter(&sr.buf, dh))
		require.NoError(t, err)

		var cl CarLookup

		cl.overrides = map[string]CarReader{
			"github.com/blah/foo": &sr,
		}

		dir2 := filepath.Join(topdir, "i")
		require.NoError(t, os.Mkdir(dir2, 0755))
		defer os.RemoveAll(dir2)

		var ci CarInstall

		ci.Lookup = &cl
		ci.Dir = dir2

		info, err := ci.Install("github.com/blah/foo", "abcdef-fake-1.0")
		require.NoError(t, err)

		testData, err := ioutil.ReadFile(filepath.Join(dir2, "abcdef-fake-1.0/bin/test"))
		require.NoError(t, err)

		assert.Equal(t, echoBin, testData)

		assert.Equal(t, "abcdef-fake-1.0", info.ID)
	})

	t.Run("installs dependencies", func(t *testing.T) {
		require.NoError(t, os.Mkdir(dir, 0755))
		defer os.RemoveAll(dir)

		require.NoError(t, os.MkdirAll(filepath.Join(dir, "a/bin"), 0755))

		err := ioutil.WriteFile(filepath.Join(dir, "a/bin/test"), testBin, 0644)
		require.NoError(t, err)

		require.NoError(t, os.MkdirAll(filepath.Join(dir, "b/bin"), 0755))

		err = ioutil.WriteFile(filepath.Join(dir, "b/bin/test"), echoBin, 0644)
		require.NoError(t, err)

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var (
			cp    CarPack
			cinfo archive.CarInfo
		)

		cp.PrivateKey = priv
		cp.PublicKey = pub
		cp.DepRootDir = dir
		cp.MapDependencies = func(s string) (string, string, string) {
			switch s {
			case fake:
				return fake + "-blah-1.0", "qux.com/pkg", base58.Encode(pub)
			default:
				panic("nope")
			}
		}

		var sr staticReader

		cinfo.ID = "abcdef-fake-1.0"

		err = cp.Pack(&cinfo, dir+"/a", &sr.buf)
		require.NoError(t, err)

		assert.Equal(t, fake+"-blah-1.0", cinfo.Dependencies[0].ID)

		var (
			cp2    CarPack
			cinfo2 archive.CarInfo
		)

		cp2.PrivateKey = priv
		cp2.PublicKey = pub

		var sr2 staticReader

		cinfo2.ID = fake + "-blah-1.0"

		err = cp2.Pack(&cinfo2, dir+"/b", &sr2.buf)
		require.NoError(t, err)

		var cl CarLookup

		cl.overrides = map[string]CarReader{
			"github.com/blah/foo": &sr,
			"qux.com/pkg":         &sr2,
		}

		dir2 := filepath.Join(topdir, "i")
		require.NoError(t, os.Mkdir(dir2, 0755))
		defer os.RemoveAll(dir2)

		var ci CarInstall

		ci.Lookup = &cl
		ci.Dir = dir2

		_, err = ci.Install("github.com/blah/foo", "abcdef-fake-1.0")
		require.NoError(t, err)

		testData, err := ioutil.ReadFile(filepath.Join(dir2, fake+"-blah-1.0/bin/test"))
		require.NoError(t, err)

		assert.Equal(t, echoBin, testData)
	})
}
