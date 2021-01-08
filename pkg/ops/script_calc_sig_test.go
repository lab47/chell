package ops

import (
	"testing"

	"github.com/lab47/exprcore/exprcore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScriptCalcSig(t *testing.T) {
	mk := func(m map[string]interface{}) *exprcore.Prototype {
		sd := make(exprcore.StringDict)

		for k, v := range m {
			var ev exprcore.Value

			switch v := v.(type) {
			case string:
				ev = exprcore.String(v)
			case int:
				ev = exprcore.MakeInt(v)
			case exprcore.Value:
				ev = v
			default:
				panic("not supported")
			}

			sd[k] = ev
		}

		return exprcore.FromStringDict(exprcore.Root, sd)
	}

	t.Run("generates a signature in the proper format", func(t *testing.T) {
		var sc ScriptCalcSig

		sig, err := sc.Calculate(mk(map[string]interface{}{
			"name":    "p1",
			"version": "0.1",
		}), nil, nil)

		require.NoError(t, err)

		assert.Regexp(t, "[a-zA-Z0-9]{20,40}-p1-0.1", sig)
	})

	t.Run("generates unique ids", func(t *testing.T) {
		var sc ScriptCalcSig

		s1, err := sc.calcSig(mk(map[string]interface{}{
			"name":    "p1",
			"version": "0.1",
		}), nil, nil)

		require.NoError(t, err)

		var sc2 ScriptCalcSig
		s2, err := sc2.calcSig(mk(map[string]interface{}{
			"name":    "p2",
			"version": "0.1",
		}), nil, nil)
		require.NoError(t, err)

		var sc3 ScriptCalcSig
		s3, err := sc3.calcSig(mk(map[string]interface{}{
			"name":    "p1",
			"version": "0.2",
		}), nil, nil)
		require.NoError(t, err)

		assert.NotEqual(t, s1, s2)
		assert.NotEqual(t, s2, s3)
		assert.NotEqual(t, s1, s3)
	})

	t.Run("supports no version being sent", func(t *testing.T) {
		var sc ScriptCalcSig
		sig, err := sc.Calculate(mk(map[string]interface{}{
			"name": "p1",
		}), nil, nil)

		require.NoError(t, err)

		var sc2 ScriptCalcSig
		sig2, err := sc2.Calculate(mk(map[string]interface{}{
			"name":    "p1",
			"version": "unknown",
		}), nil, nil)

		require.NoError(t, err)

		assert.Equal(t, sig, sig2)
	})

	t.Run("takes the inputs into account", func(t *testing.T) {
		var sc ScriptCalcSig
		sig, err := sc.Calculate(mk(map[string]interface{}{
			"name": "p1",
		}), nil, nil)

		require.NoError(t, err)

		var td testData
		td.assets = map[string][]byte{
			"p1.input": []byte("data"),
		}

		var sc2 ScriptCalcSig
		sig2, err := sc2.Calculate(mk(map[string]interface{}{
			"name": "p1",
			"input": &ScriptFile{
				path: "p1.input",
			},
		}), &td, nil)

		require.NoError(t, err)

		assert.NotEqual(t, sig, sig2)
	})

	t.Run("takes the dependencies into account", func(t *testing.T) {
		var sc ScriptCalcSig
		sig, err := sc.Calculate(mk(map[string]interface{}{
			"name": "p1",
		}), nil, nil)

		require.NoError(t, err)

		var sc2 ScriptCalcSig
		sig2, err := sc2.Calculate(mk(map[string]interface{}{
			"name": "p1",
			"dependencies": exprcore.NewList([]exprcore.Value{
				&ScriptPackage{id: "abcdef-q1-0.1"},
			}),
		}), nil, nil)

		require.NoError(t, err)

		assert.NotEqual(t, sig, sig2)
	})

}

type testData struct {
	script []byte
	assets map[string][]byte
}

func (t *testData) Script() []byte {
	return t.script
}

func (t *testData) Asset(name string) ([]byte, error) {
	x, ok := t.assets[name]
	if !ok {
		return nil, ErrNotFound
	}

	return x, nil
}

func (t *testData) Repo() string {
	return "test"
}
