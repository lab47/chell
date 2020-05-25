package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/davecgh/go-spew/spew"
	"github.com/evanphx/chell/pkg/builder"
	"github.com/evanphx/chell/pkg/chell"
	"github.com/evanphx/chell/pkg/installer"
	"github.com/evanphx/chell/pkg/lang"
	"github.com/evanphx/chell/pkg/ruby"
	"github.com/hashicorp/go-hclog"
	"github.com/spf13/pflag"
)

var (
	fName  = pflag.StringP("install", "i", "", "package to install")
	fRoot  = pflag.String("root", "", "use this directory as the chell root dir")
	fDebug = pflag.Bool("debug", false, "show debugging information")
	fTrans = pflag.Bool("translate", false, "only show the package translation")
	fBuild = pflag.Bool("build", false, "do a build")

	fPackagePath = pflag.String("pkg", ".:./packages", "pathes to search for package definitions")
)

func main() {
	pflag.Parse()

	var (
		opts chell.InstallOptions
		err  error
	)

	opts.Debug = *fDebug

	root := *fRoot
	if root != "" {
		opts, err = chell.RootedInstallOptions(root)
	} else {
		opts, err = chell.DefaultInstallOptions()
	}

	opts.PackagePath = *fPackagePath

	level := hclog.Info

	if opts.Debug {
		level = hclog.Trace
	}

	L := hclog.New(&hclog.LoggerOptions{
		Name:  "chell",
		Level: level,
	})

	ctx := hclog.WithContext(context.Background(), L)

	err = installer.Install(ctx, L, *fName, opts)
	if err != nil {
		log.Fatal(err)
	}
}

func oldmain() {
	pflag.Parse()

	var (
		opts chell.InstallOptions
		err  error
	)

	opts.Debug = *fDebug

	root := *fRoot
	if root != "" {
		opts, err = chell.RootedInstallOptions(root)
	} else {
		opts, err = chell.DefaultInstallOptions()
	}

	if err != nil {
		log.Fatal(err)
	}

	level := hclog.Info

	if opts.Debug {
		level = hclog.Trace
	}

	L := hclog.New(&hclog.LoggerOptions{
		Name:  "chell",
		Level: level,
	})

	if *fBuild {
		fn, err := lang.Locate(L, *fName, opts.StorePath, *fPackagePath)
		if err != nil {
			log.Fatal(err)
		}

		ctx := context.Background()

		L.Debug("building dependencies first")

		for _, dep := range fn.Dependencies {
			buildDir := filepath.Join(opts.CachePath, fmt.Sprintf("build-%s-%s", dep.Package.Name, dep.Package.Version))

			L.Debug("building package", "package", dep.Package.Name, "build-dir", buildDir)

			_, err := dep.Install(ctx, L, buildDir, opts.StorePath)
			if err != nil {
				log.Fatal(err)
			}
		}

		buildDir := filepath.Join(opts.CachePath, fmt.Sprintf("build-%s-%s", fn.Package.Name, fn.Package.Version))

		L.Debug("building package", "package", fn.Package.Name, "build-dir", buildDir)

		sp, err := fn.Install(ctx, L, buildDir, opts.StorePath)
		if err != nil {
			log.Fatal(err)
		}

		L.Debug("installed to store", "store-path", sp)

		return

		if false {
			spec := &builder.Spec{
				Source: "https://github.com/kkos/oniguruma/releases/download/v6.9.5_rev1/onig-6.9.5-rev1.tar.gz",
				Steps: []string{
					"./configure --disable-dependency-tracking --prefix=$prefix",
					"make",
					"make install",
				},
			}

			os.MkdirAll("./tmp/cb-auto", 0755)

			env := &builder.Env{
				BuildDir: "./tmp/cb-auto",
			}

			ctx := context.Background()

			_, err := spec.Build(ctx, L, env, nil)
			if err != nil {
				log.Fatal(err)
			}
		}

		return
	}

	if *fName == "" {
		log.Fatalln("provide a package to install")
	}

	if *fTrans {
		pkgs := map[string]*chell.Package{}

		err := os.MkdirAll(opts.TempPath, 0755)
		if err != nil {
			log.Fatal(err)
		}

		l, err := ruby.NewLoader(L, opts.TempPath)
		if err != nil {
			log.Fatal(err)
		}

		err = l.Load(filepath.Join(chell.CoreTap, *fName+".rb"), &pkgs)
		if err != nil {
			log.Fatal(err)
		}

		spew.Dump(pkgs)

		for _, pkg := range pkgs {
			f, err := lang.Translate(pkg)
			if err != nil {
				log.Fatal(err)
			}

			fmt.Printf("%s\n", f.Code)

			name := fmt.Sprintf("%s.chell", pkg.Name)

			of, err := os.Create(name)
			if err != nil {
				log.Fatal(err)
			}

			fmt.Fprintf(of, "%s\n", f.Code)

			of.Close()

			_, err = lang.Load(L, name, opts.StorePath, "")
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}
