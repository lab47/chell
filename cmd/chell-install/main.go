package main

import (
	"log"

	"github.com/evanphx/chell/pkg/chell"
	"github.com/spf13/pflag"
)

var (
	fName  = pflag.StringP("install", "i", "", "package to install")
	fRoot  = pflag.String("root", "", "use this directory as the chell root dir")
	fDebug = pflag.Bool("debug", false, "show debugging information")
)

func main() {
	pflag.Parse()

	if *fName == "" {
		log.Fatalln("provide a package to install")
	}

	var (
		opts chell.InstallOptions
		err  error
	)

	root := *fRoot
	if root != "" {
		opts, err = chell.RootedInstallOptions(root)
	} else {
		opts, err = chell.DefaultInstallOptions()
	}

	opts.Debug = *fDebug

	if err != nil {
		log.Fatal(err)
	}

	err = chell.Install(*fName, opts)
	if err != nil {
		log.Fatal(err)
	}
}
