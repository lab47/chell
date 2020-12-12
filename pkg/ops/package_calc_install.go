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
	PackageIDs   []string
	InstallOrder []string
	Installers   map[string]PackageInstaller
	Dependencies map[string][]string
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
	seen map[string]int,
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
					pti.Dependencies[pkg.ID()] = append(pti.Dependencies[pkg.ID()], cdep.ID)

					if _, ok := seen[cdep.ID]; ok {
						seen[cdep.ID]++
						continue
					}

					seen[cdep.ID] = 1

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
		pti.Dependencies[pkg.ID()] = append(pti.Dependencies[pkg.ID()], dep.ID())

		if _, ok := seen[dep.ID()]; ok {
			seen[dep.ID()]++
			continue
		}

		seen[dep.ID()] = 1

		err = p.consider(dep, pti, seen)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *PackageCalcInstall) considerCarDep(
	car *archive.CarDependency,
	pti *PackagesToInstall,
	seen map[string]int,
) error {
	installed, err := p.isInstalled(car.ID)
	if err != nil {
		return err
	}

	if installed {
		return nil
	}

	pti.PackageIDs = append(pti.PackageIDs, car.ID)

	carData, err := p.carLookup.Lookup(car.Repo, car.ID)
	if err != nil {
		return err
	}

	if carData == nil {
		return fmt.Errorf("cars can only depend on other cars, but missing: %s/%s", car.Repo, car.ID)
	}

	pti.Installers[car.ID] = &InstallCar{
		data: carData,
	}

	carInfo, err := carData.Info()
	if err != nil {
		return errors.Wrapf(err, "fetching car info: %s/%s", car.Repo, car.ID)
	}

	for _, cdep := range carInfo.Dependencies {
		pti.Dependencies[car.ID] = append(pti.Dependencies[car.ID], cdep.ID)

		if _, ok := seen[cdep.ID]; ok {
			seen[cdep.ID]++
			continue
		}

		seen[cdep.ID] = 1

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
	pti.Dependencies = make(map[string][]string)

	seen := map[string]int{
		pkg.ID(): 0,
	}

	err := p.consider(pkg, &pti, seen)
	if err != nil {
		return nil, err
	}

	var toCheck []string

	for id, deg := range seen {
		if deg == 0 {
			toCheck = append(toCheck, id)
		}
	}

	visited := 0

	var toInstall []string

	for len(toCheck) > 0 {
		x := toCheck[len(toCheck)-1]
		toCheck = toCheck[:len(toCheck)-1]

		toInstall = append(toInstall, x)

		visited++

		for _, dep := range pti.Dependencies[x] {
			deg := seen[dep] - 1
			seen[dep] = deg

			if deg == 0 {
				toCheck = append(toCheck, dep)
			}
		}
	}

	for i := len(toInstall) - 1; i >= 0; i-- {
		pti.InstallOrder = append(pti.InstallOrder, toInstall[i])
	}

	return &pti, nil
}
