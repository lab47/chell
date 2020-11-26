package ops

import (
	"path/filepath"
)

type CarInstall struct {
	Lookup *CarLookup
	Dir    string
}

func (c *CarInstall) Install(set []*CarToInstall) error {
	for _, car := range set {
		err := c.installCar(car)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *CarInstall) installCar(car *CarToInstall) error {
	r, err := car.Data.Open()
	if err != nil {
		return err
	}

	defer r.Close()

	var up CarUnpack

	err = up.Install(r, filepath.Join(c.Dir, car.ID))
	if err != nil {
		return err
	}

	return nil
}
