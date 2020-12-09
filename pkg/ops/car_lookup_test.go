package ops

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/lab47/chell/pkg/archive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticReader struct {
	name string
	buf  bytes.Buffer

	infoName string
	info     *archive.CarInfo
}

func (s *staticReader) Lookup(name string) (io.ReadCloser, error) {
	s.name = name
	return ioutil.NopCloser(&s.buf), nil
}

func (s *staticReader) Info(name string) (*archive.CarInfo, error) {
	s.infoName = name
	return s.info, nil
}

type testClient struct {
	req  []*http.Request
	resp []*http.Response
}

func (t *testClient) Do(req *http.Request) (*http.Response, error) {
	t.req = append(t.req, req)
	if len(t.resp) == 0 {
		return &http.Response{StatusCode: 404}, nil
	}

	resp := t.resp[0]
	t.resp = t.resp[1:]
	return resp, nil
}

func TestCarLookup(t *testing.T) {
	t.Run("supports overrides", func(t *testing.T) {
		var cl CarLookup

		var sr staticReader
		fmt.Fprintf(&sr.buf, "this is a car")

		sr.info = &archive.CarInfo{
			ID: "abcdef-foo-1.0",
		}

		cl.overrides = map[string]CarReader{
			"github.com/blah/foo": &sr,
		}

		car, err := cl.Lookup("github.com/blah/foo", "abcdef-foo-1.0")
		require.NoError(t, err)

		info, err := car.Info()
		require.NoError(t, err)

		assert.Equal(t, "abcdef-foo-1.0", info.ID)

		r, err := car.Open()
		require.NoError(t, err)

		defer r.Close()

		data, err := ioutil.ReadAll(r)
		require.NoError(t, err)

		assert.Equal(t, "this is a car", string(data))
	})

	t.Run("can load data from github releases", func(t *testing.T) {
		var cl CarLookup

		var tc testClient
		tc.resp = append(tc.resp,
			&http.Response{
				Body:       ioutil.NopCloser(strings.NewReader("")),
				StatusCode: 404,
			},
			&http.Response{
				Body:       ioutil.NopCloser(strings.NewReader("this is a car")),
				StatusCode: 200,
			},
			&http.Response{
				Body:       ioutil.NopCloser(strings.NewReader("this is a car")),
				StatusCode: 200,
			})

		cl.client = &tc

		car, err := cl.Lookup("github.com/blah/foo", "abcdef-foo-1.0")
		require.NoError(t, err)

		assert.Equal(t, "HEAD", tc.req[1].Method)

		assert.Equal(
			t,
			"https://github.com/blah/foo/releases/download/1.0/abcdef-foo-1.0.car",
			tc.req[1].URL.String(),
		)

		r, err := car.Open()
		require.NoError(t, err)

		defer r.Close()

		data, err := ioutil.ReadAll(r)
		require.NoError(t, err)

		assert.Equal(t, "this is a car", string(data))

		assert.Equal(t, "GET", tc.req[2].Method)

		assert.Equal(
			t,
			"https://github.com/blah/foo/releases/download/1.0/abcdef-foo-1.0.car",
			tc.req[2].URL.String(),
		)

		tc.resp = append(tc.resp, &http.Response{
			Body:       ioutil.NopCloser(strings.NewReader(`{"ID":"abcdef-foo-1.0"}`)),
			StatusCode: 200,
		})

		cinfo, err := car.Info()
		require.NoError(t, err)

		assert.Equal(t, "abcdef-foo-1.0", cinfo.ID)

		assert.Equal(t, "GET", tc.req[3].Method)

		assert.Equal(
			t,
			"https://github.com/blah/foo/releases/download/1.0/abcdef-foo-1.0.car-info.json",
			tc.req[3].URL.String(),
		)
	})

	t.Run("loads a repo config to use ", func(t *testing.T) {
		var cl CarLookup

		content := base64.StdEncoding.EncodeToString([]byte(`
{
	"car_urls": ["https://s3.foobar.com/packages"]
}
	`))

		var tc testClient
		tc.resp = append(tc.resp,
			&http.Response{
				Body: ioutil.NopCloser(strings.NewReader(fmt.Sprintf(`
{
	"content": "%s"
}
`, content))),
				StatusCode: 200,
			})
		cl.client = &tc

		car, err := cl.Lookup("github.com/blah/foo", "abcdef-foo-1.0")
		require.NoError(t, err)

		assert.Equal(t, "GET", tc.req[0].Method)

		assert.Equal(
			t,
			"https://api.github.com/repos/blah/foo/contents/chell.json",
			tc.req[0].URL.String(),
		)

		tc.req = nil
		tc.resp = nil

		tc.resp = append(tc.resp, &http.Response{
			Body:       ioutil.NopCloser(strings.NewReader("this is a car")),
			StatusCode: 200,
		})

		r, err := car.Open()
		require.NoError(t, err)

		require.NotNil(t, r)

		defer r.Close()

		data, err := ioutil.ReadAll(r)
		require.NoError(t, err)

		assert.Equal(t, "this is a car", string(data))

		assert.Equal(t, "GET", tc.req[0].Method)

		assert.Equal(
			t,
			"https://s3.foobar.com/packages/abcdef-foo-1.0.car",
			tc.req[0].URL.String(),
		)

		tc.resp = append(tc.resp, &http.Response{
			Body:       ioutil.NopCloser(strings.NewReader(`{"ID":"abcdef-foo-1.0"}`)),
			StatusCode: 200,
		})

		cinfo, err := car.Info()
		require.NoError(t, err)

		assert.Equal(t, "abcdef-foo-1.0", cinfo.ID)

		assert.Equal(t, "GET", tc.req[1].Method)

		assert.Equal(
			t,
			"https://s3.foobar.com/packages/abcdef-foo-1.0.car-info.json",
			tc.req[1].URL.String(),
		)
	})

	t.Run("supports vanity urls via HTML", func(t *testing.T) {
		var cl CarLookup

		content := base64.StdEncoding.EncodeToString([]byte(`
{
	"car_urls": ["https://s3.foobar.com/packages"]
}
	`))

		var tc testClient
		tc.resp = append(tc.resp,
			&http.Response{
				Body: ioutil.NopCloser(strings.NewReader(`
<!DOCTYPE html>
<html>
<head>
<meta name="chell-import" content="blah.com/foo git github.com/blah/foo">
</head>
<body>
</body>
</html>
`)),
				StatusCode: 200,
			},
			&http.Response{
				Body: ioutil.NopCloser(strings.NewReader(fmt.Sprintf(`
{
	"content": "%s"
}
`, content))),
				StatusCode: 200,
			},
		)
		cl.client = &tc

		car, err := cl.Lookup("blah.com/foo", "abcdef-foo-1.0")
		require.NoError(t, err)

		assert.Equal(t, "GET", tc.req[0].Method)

		assert.Equal(
			t,
			"https://blah.com/foo?chell-get=1",
			tc.req[0].URL.String(),
		)

		assert.Equal(t, "GET", tc.req[1].Method)

		assert.Equal(
			t,
			"https://api.github.com/repos/blah/foo/contents/chell.json",
			tc.req[1].URL.String(),
		)

		tc.req = nil
		tc.resp = nil

		tc.resp = append(tc.resp, &http.Response{
			Body:       ioutil.NopCloser(strings.NewReader("this is a car")),
			StatusCode: 200,
		})

		r, err := car.Open()
		require.NoError(t, err)

		require.NotNil(t, r)

		defer r.Close()

		data, err := ioutil.ReadAll(r)
		require.NoError(t, err)

		assert.Equal(t, "this is a car", string(data))

		assert.Equal(t, "GET", tc.req[0].Method)

		assert.Equal(
			t,
			"https://s3.foobar.com/packages/abcdef-foo-1.0.car",
			tc.req[0].URL.String(),
		)
	})
}
