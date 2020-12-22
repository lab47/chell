package ops

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type funcPkgInstaller func(ctx context.Context, ienv *InstallEnv) error

func (f funcPkgInstaller) Install(ctx context.Context, ienv *InstallEnv) error {
	return f(ctx, ienv)
}

func TestPackagesInstall(t *testing.T) {
	t.Run("runs install functions", func(t *testing.T) {
		var ti PackagesToInstall

		testEnv := &InstallEnv{
			StoreDir: "/nonexistant",
			BuildDir: "/also-nonexistant",
		}

		var called bool

		pi := func(ctx context.Context, ienv *InstallEnv) error {
			called = true
			assert.Equal(t, testEnv, ienv)
			return nil
		}

		ti.InstallOrder = []string{"xyz-a-1.0"}
		ti.Installers = map[string]PackageInstaller{
			"xyz-a-1.0": funcPkgInstaller(pi),
		}

		var pkginst PackagesInstall
		pkginst.ienv = testEnv

		err := pkginst.Install(context.TODO(), &ti)
		require.NoError(t, err)

		assert.True(t, called)

		assert.Equal(t, []string{"xyz-a-1.0"}, pkginst.Installed)
	})

	t.Run("skips packages that are already installed", func(t *testing.T) {
		var ti PackagesToInstall

		dir, err := ioutil.TempDir("", "chell")
		require.NoError(t, err)

		defer os.RemoveAll(dir)

		testEnv := &InstallEnv{
			StoreDir: dir,
			BuildDir: "/also-nonexistant",
		}

		var called bool

		pi := func(ctx context.Context, ienv *InstallEnv) error {
			called = true
			assert.Equal(t, testEnv, ienv)
			return nil
		}

		ti.InstallOrder = []string{"xyz-a-1.0"}
		ti.Installers = map[string]PackageInstaller{
			"xyz-a-1.0": funcPkgInstaller(pi),
		}

		err = os.MkdirAll(filepath.Join(dir, "xyz-a-1.0", "bin"), 0755)
		require.NoError(t, err)

		var pkginst PackagesInstall
		pkginst.ienv = testEnv

		err = pkginst.Install(context.TODO(), &ti)
		require.NoError(t, err)

		assert.False(t, called)

		assert.Nil(t, pkginst.Installed)
	})

	t.Run("deletes a store dir when install fails", func(t *testing.T) {
		var ti PackagesToInstall

		dir, err := ioutil.TempDir("", "chell")
		require.NoError(t, err)

		defer os.RemoveAll(dir)

		testEnv := &InstallEnv{
			StoreDir: dir,
			BuildDir: "/also-nonexistant",
		}

		var called bool

		pi := func(ctx context.Context, ienv *InstallEnv) error {
			called = true
			assert.Equal(t, testEnv, ienv)

			err := os.MkdirAll(filepath.Join(ienv.StoreDir, "xyz-a-1.0", "bin"), 0755)
			require.NoError(t, err)

			return io.EOF
		}

		ti.InstallOrder = []string{"xyz-a-1.0"}
		ti.Installers = map[string]PackageInstaller{
			"xyz-a-1.0": funcPkgInstaller(pi),
		}

		var pkginst PackagesInstall
		pkginst.ienv = testEnv

		err = pkginst.Install(context.TODO(), &ti)
		assert.Error(t, err)

		assert.True(t, called)

		assert.Nil(t, pkginst.Installed)
		assert.Equal(t, "xyz-a-1.0", pkginst.Failed)

		_, err = os.Stat(filepath.Join(dir, "xyz-a-1.0"))
		assert.Error(t, err)
	})
}
