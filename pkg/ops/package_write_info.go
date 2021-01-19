package ops

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/lab47/chell/pkg/data"
	"github.com/pkg/errors"
)

type PackageWriteInfo struct {
	storeDir string
}

func (p *PackageWriteInfo) Write(pkg *ScriptPackage) (*data.PackageInfo, error) {
	path := filepath.Join(p.storeDir, pkg.ID(), ".pkg-info.json")

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var sfd StoreFindDeps
	sfd.storeDir = p.storeDir

	var d ScriptCalcDeps

	allDeps, err := d.BuildDeps(pkg)
	if err != nil {
		return nil, err
	}

	runtimeDeps, err := sfd.PruneDeps(pkg.ID(), allDeps)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to prune deps")
	}

	var depIds []string
	for _, dep := range runtimeDeps {
		depIds = append(depIds, dep.ID())
	}

	var buildDeps []string
	for _, dep := range allDeps {
		buildDeps = append(buildDeps, dep.ID())
	}

	var inputs []*data.PackageInput

	for _, input := range pkg.cs.Inputs {
		d := &data.PackageInput{
			Name:    input.Name,
			SumType: input.Data.sumType,
			Sum:     input.Data.sumValue,
		}

		if input.Data.dir != "" {
			d.Dir = input.Data.dir
		} else {
			d.Path = input.Data.path
		}

		inputs = append(inputs, d)
	}

	pi := &data.PackageInfo{
		Id:          pkg.ID(),
		Name:        pkg.Name(),
		Version:     pkg.Version(),
		Repo:        pkg.Repo(),
		RuntimeDeps: depIds,
		BuildDeps:   buildDeps,
		Constraints: pkg.Constraints(),
		Inputs:      inputs,
	}

	err = json.NewEncoder(f).Encode(&pi)

	pkg.PackageInfo = pi

	return pi, err
}
