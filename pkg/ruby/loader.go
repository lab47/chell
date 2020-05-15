package ruby

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
)

type Loader struct {
	L   hclog.Logger
	dir string
}

func NewLoader(L hclog.Logger, path string) (*Loader, error) {
	dir, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	L.Trace("load ruby assets to disk")

	err = RestoreAssets(dir, "lib")
	if err != nil {
		return nil, err
	}

	err = RestoreAssets(dir, "cmd")
	if err != nil {
		return nil, err
	}

	return &Loader{L: L, dir: dir}, nil
}

func (l *Loader) Load(path string, v interface{}) error {
	l.L.Trace("loading homebrew ruby for translation", "path", path)

	cmd := exec.Command("ruby", filepath.Join(l.dir, "./cmd/run.rb"), path)
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return err
	}

	return json.Unmarshal(data, v)
}
