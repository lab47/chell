package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/evanphx/chell/pkg/builder"
	"github.com/evanphx/chell/pkg/chell"
	"github.com/evanphx/chell/pkg/lang"
	"github.com/hashicorp/go-hclog"
)

func Install(ctx context.Context, L hclog.Logger, name string, opts chell.InstallOptions) error {
	var n Native
	n.L = L

	return n.Install(ctx, name, opts)
}

type Native struct {
	L hclog.Logger
}

type toProcess struct {
	fn       *lang.Function
	requests []string
}

func (n *Native) calculateDegree(fn *lang.Function) (map[string]int, map[string]*toProcess) {
	degree := map[string]int{}
	named := map[string]*toProcess{}

	toCheck := []*lang.Function{fn}

	degree[fn.Package.Name] = 0

	for len(toCheck) > 0 {
		x := toCheck[len(toCheck)-1]
		toCheck = toCheck[:len(toCheck)-1]

		named[x.Package.Name] = &toProcess{fn: x}

		for _, dep := range x.Dependencies {
			if deg, ok := degree[dep.Package.Name]; ok {
				degree[dep.Package.Name] = deg + 1
			} else {
				degree[dep.Package.Name] = 1
				toCheck = append(toCheck, dep)
			}
		}
	}

	return degree, named
}

func (n *Native) calculateFunctions(name string, opts chell.InstallOptions) ([]*toProcess, error) {
	var (
		toInstall []*toProcess
		toCheck   []*toProcess
	)

	fn, err := lang.Locate(n.L, name, opts.StorePath, opts.PackagePath)
	if err != nil {
		return nil, err
	}

	degree, named := n.calculateDegree(fn)

	for k, deg := range degree {
		if deg == 0 {
			x := named[k]
			x.requests = []string{"<user>"}
			toCheck = append(toCheck, x)
		}
	}

	visited := 0

	for len(toCheck) > 0 {
		x := toCheck[len(toCheck)-1]
		toCheck = toCheck[:len(toCheck)-1]

		toInstall = append(toInstall, x)

		visited++

		for _, dep := range x.fn.Dependencies {
			sub := named[dep.Package.Name]
			sub.requests = append(sub.requests, x.fn.Package.Name)

			deg := degree[dep.Package.Name] - 1
			degree[dep.Package.Name] = deg

			if deg == 0 {
				toCheck = append(toCheck, sub)
			}
		}
	}

	// reverse toInstall for the convienence of the caller
	var out []*toProcess

	for i := len(toInstall) - 1; i >= 0; i-- {
		out = append(out, toInstall[i])
	}

	return out, nil
}

func (n *Native) calculateStoreNames(fns []*toProcess) (map[string]string, error) {
	names := map[string]string{}

	ctx := hclog.WithContext(context.Background(), n.L)

	for _, tp := range fns {
		storeName, err := tp.fn.StoreName(ctx)
		if err != nil {
			return nil, err
		}

		names[tp.fn.Package.Name] = storeName
	}

	return names, nil
}

func (n *Native) checkInstalled(storeName string, opts chell.InstallOptions) bool {
	_, err := os.Stat(filepath.Join(opts.StorePath, storeName, builder.ManifestName))
	return err == nil
}

func (n *Native) Install(ctx context.Context, name string, opts chell.InstallOptions) error {
	n.L.Debug("calculating functions to install")

	tps, err := n.calculateFunctions(name, opts)
	if err != nil {
		return err
	}

	names, err := n.calculateStoreNames(tps)
	if err != nil {
		return err
	}

	tr := tabwriter.NewWriter(os.Stdout, 2, 2, 1, ' ', 0)

	for i, tp := range tps {
		k := tp.fn.Package.Name
		fmt.Fprintf(tr, "% 3d: %s\t%s\t%s\n", i, k, names[k], strings.Join(tp.requests, ", "))
	}

	tr.Flush()

	n.L.Debug("calculated packages it install", "count", len(tps))

	for _, tp := range tps {
		fn := tp.fn

		storeName, err := fn.StoreName(ctx)
		if err != nil {
			return err
		}

		if n.checkInstalled(storeName, opts) {
			n.L.Info("using installed package", "name", fn.Package.Name, "version", fn.Package.Version, "store-name", storeName)
		} else {
			buildDir := filepath.Join(opts.CachePath, fmt.Sprintf("build-%s-%s", fn.Package.Name, fn.Package.Version))

			n.L.Debug("building package", "package", fn.Package.Name, "build-dir", buildDir)

			sp, err := fn.Install(ctx, n.L, buildDir, opts.StorePath)
			if err != nil {
				return err
			}

			n.L.Debug("installed to store", "store-path", sp)
		}
	}

	return nil
}
