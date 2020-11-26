package ops

import (
	"fmt"
	"os"
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

	tg := filepath.Join(c.Dir, car.ID)

	err = up.Install(r, tg)
	if err != nil {
		return err
	}

	if up.Info.Signer != car.Signer {
		os.RemoveAll(tg)
		return fmt.Errorf("car signer not the same as indicated in the dependency entry: %s != %s", up.Info.Signer, car.Signer)
	}

	return nil
}
