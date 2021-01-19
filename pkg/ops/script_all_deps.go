package ops

type ScriptCalcDeps struct {
	storeDir string
}

func (i *ScriptCalcDeps) pkgRuntimeDeps(pkg *ScriptPackage) ([]*ScriptPackage, error) {
	runtimeDeps := pkg.Dependencies()

	var pri PackageReadInfo
	pri.storeDir = i.storeDir

	pi, err := pri.Read(pkg)
	if err != nil {
		return nil, err
	}
	var pruned []*ScriptPackage

outer:
	for _, sp := range runtimeDeps {
		for _, id := range pi.RuntimeDeps {
			if id == sp.ID() {
				pruned = append(pruned, sp)
				continue outer
			}
		}
	}

	runtimeDeps = pruned

	return runtimeDeps, nil
}

func (i *ScriptCalcDeps) RuntimeDeps(pkg *ScriptPackage) ([]*ScriptPackage, error) {
	direct, err := i.pkgRuntimeDeps(pkg)
	if err != nil {
		return nil, err
	}

	return i.walkFromDeps(direct)
}

func (i *ScriptCalcDeps) BuildDeps(pkg *ScriptPackage) ([]*ScriptPackage, error) {
	return i.walkFromDeps(pkg.Dependencies())
}

func (i *ScriptCalcDeps) walkFromDeps(deps []*ScriptPackage) ([]*ScriptPackage, error) {
	seen := make(map[string]struct{})

	var output []*ScriptPackage

	for len(deps) > 0 {
		dep := deps[0]
		deps = deps[1:]

		if _, ok := seen[dep.ID()]; ok {
			continue
		}

		seen[dep.ID()] = struct{}{}

		output = append(output, dep)

		runtimDeps, err := i.pkgRuntimeDeps(dep)
		if err != nil {
			return nil, err
		}

		for _, x := range runtimDeps {
			if _, ok := seen[x.ID()]; ok {
				continue
			}

			deps = append(deps, x)
		}
	}

	return output, nil
}
