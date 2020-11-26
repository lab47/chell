package ops

import (
	"path/filepath"

	"github.com/lab47/chell/pkg/archive"
)

type CarInstall struct {
	Lookup *CarLookup
	Dir    string
}

func (c *CarInstall) Install(repo, name string) (*archive.CarInfo, error) {
	cd, err := c.Lookup.Lookup(repo, name)
	if err != nil {
		return nil, err
	}

	r, err := cd.Open()
	if err != nil {
		return nil, err
	}

	defer r.Close()

	var up CarUnpack

	err = up.Install(r, filepath.Join(c.Dir, name))
	if err != nil {
		return nil, err
	}

	for _, dep := range up.Info.Dependencies {
		_, err = c.Install(dep.Repo, dep.ID)
		if err != nil {
			return nil, err
		}
	}

	return &up.Info, nil
}
