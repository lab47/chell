package ops

import "github.com/lab47/chell/pkg/archive"

type CarCalcSet struct {
	Lookup *CarLookup
}

type CarToInstall struct {
	Repo string
	ID   string
	Info *archive.CarInfo
	Data *CarData
}

func (c *CarCalcSet) Calculate(repo, name string) ([]*CarToInstall, error) {
	start := &CarToInstall{
		Repo: repo,
		ID:   name,
	}

	var (
		toInstall = []*CarToInstall{}
		toProcess = []*CarToInstall{start}
		seen      = map[string]struct{}{
			name: {},
		}
	)

	for len(toProcess) > 0 {
		x := toProcess[0]
		toProcess = toProcess[1:]

		cd, err := c.Lookup.Lookup(x.Repo, x.ID)
		if err != nil {
			return nil, err
		}

		info, err := cd.Info()
		if err != nil {
			return nil, err
		}

		x.Data = cd
		x.Info = info

		seen[x.ID] = struct{}{}

		for _, dep := range x.Info.Dependencies {
			if _, ok := seen[dep.ID]; ok {
				continue
			}

			toProcess = append(toProcess, &CarToInstall{
				Repo: dep.Repo,
				ID:   dep.ID,
			})
		}

		toInstall = append(toInstall, x)
	}

	return toInstall, nil
}
