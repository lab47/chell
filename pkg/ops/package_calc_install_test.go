package ops

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/lab47/chell/pkg/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pkgCarReader struct {
	data map[string][]byte
	info map[string]*data.CarInfo
}

func (p *pkgCarReader) Lookup(name string) (io.ReadCloser, error) {
	buf, ok := p.data[name]
	if !ok {
		return nil, NoCarData
	}

	return ioutil.NopCloser(bytes.NewReader(buf)), nil
}

func (p *pkgCarReader) Info(name string) (*data.CarInfo, error) {
	info, ok := p.info[name]
	if !ok {
		return nil, NoCarData
	}

	return info, nil
}

func TestPackageCalcInstall(t *testing.T) {
	top, err := ioutil.TempDir("", "chell")
	require.NoError(t, err)

	defer os.RemoveAll(top)

	t.Run("returns a script with install func", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p1")
		require.NoError(t, err)

		var pci PackageCalcInstall

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		assert.Equal(t, []string{pkg.ID()}, toInstall.PackageIDs)
	})

	t.Run("prunes packages that are already installed", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p1")
		require.NoError(t, err)

		storeDir := filepath.Join(top, "store")

		sd := filepath.Join(storeDir, pkg.ID())

		err = os.MkdirAll(sd, 0755)
		require.NoError(t, err)

		defer os.RemoveAll(storeDir)

		var pci PackageCalcInstall
		pci.StoreDir = storeDir

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		assert.Equal(t, 0, len(toInstall.PackageIDs))
	})

	t.Run("includes declared dependencies", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p2")
		require.NoError(t, err)

		var pci PackageCalcInstall

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		spew.Dump(sl)

		p1 := sl.loaded["p1"]
		require.NotNil(t, p1)

		assert.Equal(t, []string{pkg.ID(), p1.ID()}, toInstall.PackageIDs)
	})

	t.Run("computes the install order", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p2")
		require.NoError(t, err)

		var pci PackageCalcInstall

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		p1 := sl.loaded["p1"]
		require.NotNil(t, p1)

		assert.Equal(t, []string{p1.ID(), pkg.ID()}, toInstall.InstallOrder)
	})

	t.Run("computes the install order when deps seen multiple times", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p4")
		require.NoError(t, err)

		var pci PackageCalcInstall

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		p1 := sl.loaded["p1"]
		require.NotNil(t, p1)

		p2 := sl.loaded["p2"]
		require.NotNil(t, p2)

		assert.Equal(t, []string{p1.ID(), p2.ID(), pkg.ID()}, toInstall.InstallOrder)
	})

	t.Run("includes declared dependencies recursively", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p3")
		require.NoError(t, err)

		var pci PackageCalcInstall

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		p1 := sl.loaded["p1"]
		require.NotNil(t, p1)

		p2 := sl.loaded["p2"]
		require.NotNil(t, p2)

		assert.Equal(t, []string{pkg.ID(), p2.ID(), p1.ID()}, toInstall.PackageIDs)

	})

	t.Run("skips all deps if a package is installed", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl0 ScriptLoad
		sl0.lookup = &lookup

		p2, err := sl0.Load("p2")
		require.NoError(t, err)

		storeDir := filepath.Join(top, "store")

		sd := filepath.Join(storeDir, p2.ID())

		err = os.MkdirAll(sd, 0755)
		require.NoError(t, err)

		defer os.RemoveAll(storeDir)

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p3")
		require.NoError(t, err)

		var pci PackageCalcInstall
		pci.StoreDir = storeDir

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		assert.Equal(t, []string{pkg.ID()}, toInstall.PackageIDs)
	})

	t.Run("only includes each dep once", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p4")
		require.NoError(t, err)

		var pci PackageCalcInstall

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		p1 := sl.loaded["p1"]
		require.NotNil(t, p1)

		p2 := sl.loaded["p2"]
		require.NotNil(t, p2)

		assert.Equal(t, []string{pkg.ID(), p2.ID(), p1.ID()}, toInstall.PackageIDs)
	})

	t.Run("generates a script installer value", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p1")
		require.NoError(t, err)

		var pci PackageCalcInstall

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		iv, ok := toInstall.Installers[toInstall.PackageIDs[0]]
		require.True(t, ok)

		_, ok = iv.(*ScriptInstall)
		require.True(t, ok)
	})

	t.Run("tries to use a car", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sr staticReader
		fmt.Fprintf(&sr.buf, "this is a car")

		sr.info = &data.CarInfo{}

		var tc testClient

		var carLookup CarLookup
		carLookup.overrides = map[string]CarReader{
			"": &sr,
		}
		carLookup.client = &tc

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p1")
		require.NoError(t, err)

		var pci PackageCalcInstall
		pci.carLookup = &carLookup

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		iv, ok := toInstall.Installers[toInstall.PackageIDs[0]]
		require.True(t, ok)

		_, ok = iv.(*InstallCar)
		require.True(t, ok)
	})

	t.Run("only uses the dependencies from the car", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/package_calc_install"}

		var sl0 ScriptLoad
		sl0.lookup = &lookup

		p2, err := sl0.Load("p2")
		require.NoError(t, err)

		var sr pkgCarReader

		sr.data = map[string][]byte{
			p2.ID(): []byte("this is a car"),
		}

		sr.info = map[string]*data.CarInfo{
			p2.ID(): {},
		}

		var tc testClient

		var carLookup CarLookup
		carLookup.overrides = map[string]CarReader{
			"": &sr,
		}
		carLookup.client = &tc

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("p3")
		require.NoError(t, err)

		var pci PackageCalcInstall
		pci.carLookup = &carLookup

		toInstall, err := pci.Calculate(pkg)
		require.NoError(t, err)

		assert.Equal(t, []string{pkg.ID(), p2.ID()}, toInstall.PackageIDs)

		iv, ok := toInstall.Installers[p2.ID()]
		require.True(t, ok)

		_, ok = iv.(*InstallCar)
		require.True(t, ok)

		assert.Equal(t, 2, len(toInstall.Installers))
	})

}
