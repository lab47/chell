package ops

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigLoad(t *testing.T) {
	t.Run("loads toplevel constants as repos", func(t *testing.T) {
		var cl ConfigLoad

		cfg, err := cl.Load(strings.NewReader(`
chell = repo(
	github: "lab47/chell-packages",
	path: "../packages",
)
		`))

		require.NoError(t, err)

		repo, ok := cfg.Repos["chell"]
		require.True(t, ok)

		assert.Equal(t, "lab47/chell-packages", repo.Github)
		assert.Equal(t, "../packages", repo.Path)
	})
}
