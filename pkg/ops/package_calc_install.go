package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lab47/chell/pkg/archive"
	"github.com/pkg/errors"
)

var ErrCorruption = errors.New("corruption detected")

type PackageCalcInstall struct {
	StoreDir string

	carLookup *CarLookup
}

type PackageInstaller interface {
	Install(ctx context.Context) error
}

type InstallFunction struct {
	pkg *ScriptPackage
}

func (i *InstallFunction) Install(ctx context.Context) error {
	return nil
}

type InstallCar struct {
	data *CarData
}

func (i *InstallCar) Install(ctx context.Context) error {
	return nil
}

type PackagesToInstall struct {
	PackageIDs []string
	Installers map[string]PackageInstaller
}

func (p *PackageCalcInstall) isInstalled(id string) (bool, error) {
	path := filepath.Join(p.StoreDir, id)

	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, err
	}

	if fi.IsDir() {
		return true, nil
	}

	return false, errors.Wrapf(ErrCorruption, "store path not a dir: %s", path)
}

func (p *PackageCalcInstall) consider(
	pkg *ScriptPackage,
	pti *PackagesToInstall,
	seen map[string]struct{},
) error {
	installed, err := p.isInstalled(pkg.ID())
	if err != nil {
		return err
	}

	if installed {
		return nil
	}

	pti.PackageIDs = append(pti.PackageIDs, pkg.ID())

	if p.carLookup != nil {
		carData, err := p.carLookup.Lookup(pkg.Repo(), pkg.ID())
		if err != nil {
			return errors.Wrapf(err, "error looking up car: %s/%s", pkg.Repo(), pkg.ID())
		}

		if carData != nil {
			var skip bool

			carInfo, err := carData.Info()
			if err != nil {
				if err == NoCarData {
					skip = true
				} else {
					return errors.Wrapf(err, "error looking up car info: %s/%s", pkg.Repo(), pkg.ID())
				}
			}

			if !skip {
				pti.Installers[pkg.ID()] = &InstallCar{
					data: carData,
				}

				for _, cdep := range carInfo.Dependencies {
					if _, ok := seen[cdep.ID]; ok {
						continue
					}

					seen[cdep.ID] = struct{}{}

					err = p.considerCarDep(cdep, pti, seen)
					if err != nil {
						return err
					}
				}
				return nil
			}
		}
	}

	pti.Installers[pkg.ID()] = &InstallFunction{pkg}

	for _, dep := range pkg.Dependencies() {
		if _, ok := seen[dep.ID()]; ok {
			continue
		}

		seen[dep.ID()] = struct{}{}

		err = p.consider(dep, pti, seen)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *PackageCalcInstall) considerCarDep(
	cdep *archive.CarDependency,
	pti *PackagesToInstall,
	seen map[string]struct{},
) error {
	installed, err := p.isInstalled(cdep.ID)
	if err != nil {
		return err
	}

	if installed {
		return nil
	}

	pti.PackageIDs = append(pti.PackageIDs, cdep.ID)

	carData, err := p.carLookup.Lookup(cdep.Repo, cdep.ID)
	if err != nil {
		return err
	}

	if carData == nil {
		return fmt.Errorf("cars can only depend on other cars, but missing: %s/%s", cdep.Repo, cdep.ID)
	}

	pti.Installers[cdep.ID] = &InstallCar{
		data: carData,
	}

	carInfo, err := carData.Info()
	if err != nil {
		return err
	}

	for _, cdep := range carInfo.Dependencies {
		if _, ok := seen[cdep.ID]; ok {
			continue
		}

		seen[cdep.ID] = struct{}{}

		err = p.considerCarDep(cdep, pti, seen)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *PackageCalcInstall) Calculate(pkg *ScriptPackage) (*PackagesToInstall, error) {
	var pti PackagesToInstall
	pti.Installers = make(map[string]PackageInstaller)

	seen := map[string]struct{}{}

	err := p.consider(pkg, &pti, seen)
	if err != nil {
		return nil, err
	}

	return &pti, nil
}
