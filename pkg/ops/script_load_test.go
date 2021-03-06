package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScriptLoad(t *testing.T) {
	t.Run("evaluates a script and provides package info", func(t *testing.T) {
		var (
			load   ScriptLoad
			lookup ScriptLookup
		)

		lookup.Path = []string{"./testdata/script_load"}

		load.lookup = &lookup

		pkg, err := load.Load("p1")
		require.NoError(t, err)

		assert.Regexp(t, ".*-p1-1.0", pkg.ID())
	})

	t.Run("can process inputs", func(t *testing.T) {
		var (
			load   ScriptLoad
			lookup ScriptLookup
		)

		lookup.Path = []string{"./testdata/script_load"}

		load.lookup = &lookup

		pkg, err := load.Load("p2")
		require.NoError(t, err)

		assert.Regexp(t, ".*-p1-1.0", pkg.ID())
	})

	t.Run("makes helpers available", func(t *testing.T) {
		var (
			load   ScriptLoad
			lookup ScriptLookup
		)

		lookup.Path = []string{"./testdata/script_load"}

		load.lookup = &lookup

		pkg, err := load.Load("p3")
		require.NoError(t, err)

		assert.Regexp(t, ".*-helpermade-p3.0", pkg.ID())
	})

	t.Run("supports packages reporting their install prefix", func(t *testing.T) {
		var (
			load   ScriptLoad
			lookup ScriptLookup
		)

		lookup.Path = []string{"./testdata/script_load"}

		load.lookup = &lookup

		pkg, err := load.Load("ip1")
		require.NoError(t, err)

		assert.Regexp(t, ".*-ip1-1.0", pkg.ID())
	})

	t.Run("supports passing named arguments to a package", func(t *testing.T) {
		var (
			load   ScriptLoad
			lookup ScriptLookup
		)

		lookup.Path = []string{"./testdata/script_load"}

		load.lookup = &lookup

		pkg, err := load.Load("v1", WithArgs(map[string]string{
			"version": "2.7.7",
		}))

		require.NoError(t, err)

		assert.Regexp(t, ".*-v1-2.7.7", pkg.ID())
	})
}
