package ops

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScriptLookup(t *testing.T) {
	t.Run("can load a script from a directory", func(t *testing.T) {
		var sl ScriptLookup

		sd, err := sl.loadDir("./testdata", "foo")
		require.NoError(t, err)

		expected, err := ioutil.ReadFile("./testdata/foo.chell")
		require.NoError(t, err)

		assert.Equal(t, expected, sd.Script())
	})

	t.Run("can load a script from a packages directory", func(t *testing.T) {
		var sl ScriptLookup

		sd, err := sl.loadDir("./testdata", "foo2")
		require.NoError(t, err)

		expected, err := ioutil.ReadFile("./testdata/packages/foo2.chell")
		require.NoError(t, err)

		assert.Equal(t, expected, sd.Script())
	})

	t.Run("can load a script from a named directory", func(t *testing.T) {
		var sl ScriptLookup

		sd, err := sl.loadDir("./testdata", "foo3")
		require.NoError(t, err)

		expected, err := ioutil.ReadFile("./testdata/packages/foo3/foo3.chell")
		require.NoError(t, err)

		assert.Equal(t, expected, sd.Script())
	})

	t.Run("can load a script from a sharded directory", func(t *testing.T) {
		var sl ScriptLookup

		sd, err := sl.loadDir("./testdata", "foo4")
		require.NoError(t, err)

		expected, err := ioutil.ReadFile("./testdata/packages/fo/foo4.chell")
		require.NoError(t, err)

		assert.Equal(t, expected, sd.Script())
	})

	t.Run("can load a script from a named sharded directory", func(t *testing.T) {
		var sl ScriptLookup

		sd, err := sl.loadDir("./testdata", "foo5")
		require.NoError(t, err)

		expected, err := ioutil.ReadFile("./testdata/packages/fo/foo5/foo5.chell")
		require.NoError(t, err)

		assert.Equal(t, expected, sd.Script())
	})

	t.Run("can load a script from github", func(t *testing.T) {
		var sl ScriptLookup

		cases := []struct {
			path  string
			depth int
		}{
			{
				path: "foo.chell",
			},
			{
				path:  "packages/foo.chell",
				depth: 1,
			},
			{
				path:  "packages/foo/foo.chell",
				depth: 2,
			},
			{
				path:  "packages/fo/foo.chell",
				depth: 3,
			},
			{
				path:  "packages/fo/foo/foo.chell",
				depth: 4,
			},
		}

		for _, c := range cases {
			var tc testClient

			fdata, err := ioutil.ReadFile("./testdata/foo.chell")
			require.NoError(t, err)

			for i := 0; i < c.depth; i++ {
				tc.resp = append(tc.resp, &http.Response{
					Body:       ioutil.NopCloser(strings.NewReader("")),
					StatusCode: 404,
				})
			}

			tc.resp = append(tc.resp, &http.Response{
				Body: ioutil.NopCloser(strings.NewReader(
					fmt.Sprintf(`{"content": "%s"}`, base64.StdEncoding.EncodeToString(fdata)),
				)),
				StatusCode: 200,
			})

			sd, err := sl.loadGithub(&tc, "github.com/blah/pkgs", "foo")
			require.NoError(t, err)

			i := 0

			for ; i < c.depth; i++ {
				assert.Equal(t, "GET", tc.req[i].Method)
			}

			assert.Equal(t, "GET", tc.req[i].Method)

			assert.Equal(
				t,
				"https://api.github.com/repos/blah/pkgs/contents/"+c.path,
				tc.req[i].URL.String(),
			)

			expected, err := ioutil.ReadFile("./testdata/foo.chell")
			require.NoError(t, err)

			assert.Equal(t, expected, sd.Script())
		}
	})

	t.Run("can load a script from a vanity domain", func(t *testing.T) {
		var tc testClient

		fdata, err := ioutil.ReadFile("./testdata/foo.chell")
		require.NoError(t, err)

		tc.resp = append(tc.resp,
			&http.Response{
				Body: ioutil.NopCloser(strings.NewReader(`
<!DOCTYPE html>
<html>
<head>
<meta name="chell-import" content="blah.com/pkg git github.com/blah/foo">
</head>
<body>
</body>
</html>
`)),
				StatusCode: 200,
			},
			&http.Response{
				Body: ioutil.NopCloser(strings.NewReader(
					fmt.Sprintf(`{"content": "%s"}`, base64.StdEncoding.EncodeToString(fdata)),
				)),
				StatusCode: 200,
			},
		)

		var sl ScriptLookup
		sl.client = &tc

		sd, err := sl.loadVanity(&tc, "blah.com/pkg", "foo")
		require.NoError(t, err)

		assert.Equal(t, "GET", tc.req[1].Method)

		assert.Equal(
			t,
			"https://api.github.com/repos/blah/foo/contents/foo.chell",
			tc.req[1].URL.String(),
		)

		expected, err := ioutil.ReadFile("./testdata/foo.chell")
		require.NoError(t, err)

		assert.Equal(t, expected, sd.Script())
	})

	t.Run("supports a path for loading of a number of places", func(t *testing.T) {
		var sl ScriptLookup

		sl.Path = []string{"github.com/foo/bar", "./testdata"}

		var tc testClient

		for i := 0; i < 5; i++ {
			tc.resp = append(tc.resp, &http.Response{
				Body:       ioutil.NopCloser(strings.NewReader("")),
				StatusCode: 404,
			})
		}

		sl.client = &tc

		sd, err := sl.Load("foo")
		require.NoError(t, err)

		expected, err := ioutil.ReadFile("./testdata/foo.chell")
		require.NoError(t, err)

		assert.Equal(t, expected, sd.Script())
	})

	t.Run("can load assets", func(t *testing.T) {
		var sl ScriptLookup

		sd, err := sl.loadDir("./testdata", "foo")
		require.NoError(t, err)

		data, err := sd.Asset("blah.patch")
		require.NoError(t, err)

		expected, err := ioutil.ReadFile("./testdata/blah.patch")
		require.NoError(t, err)

		assert.Equal(t, expected, data)
	})

	t.Run("can load a assets from github", func(t *testing.T) {
		var sl ScriptLookup

		var tc testClient

		fdata, err := ioutil.ReadFile("./testdata/foo.chell")
		require.NoError(t, err)

		tc.resp = append(tc.resp, &http.Response{
			Body: ioutil.NopCloser(strings.NewReader(
				fmt.Sprintf(`{"content": "%s"}`, base64.StdEncoding.EncodeToString(fdata)),
			)),
			StatusCode: 200,
		})

		tc.resp = append(tc.resp, &http.Response{
			Body: ioutil.NopCloser(strings.NewReader(
				fmt.Sprintf(`{"content": "%s"}`,
					base64.StdEncoding.EncodeToString([]byte("this is a patch"))),
			)),
			StatusCode: 200,
		})

		sd, err := sl.loadGithub(&tc, "github.com/blah/pkgs", "foo")
		require.NoError(t, err)

		assert.Equal(t, "GET", tc.req[0].Method)

		assert.Equal(
			t,
			"https://api.github.com/repos/blah/pkgs/contents/foo.chell",
			tc.req[0].URL.String(),
		)

		expected, err := ioutil.ReadFile("./testdata/foo.chell")
		require.NoError(t, err)

		assert.Equal(t, expected, sd.Script())

		pdata, err := sd.Asset("blah.patch")

		assert.Equal(t, "GET", tc.req[1].Method)

		assert.Equal(
			t,
			"https://api.github.com/repos/blah/pkgs/contents/blah.patch",
			tc.req[1].URL.String(),
		)

		assert.Equal(t, "this is a patch", string(pdata))
	})
}
