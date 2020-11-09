package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/archive"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/loader"
	"github.com/lab47/chell/pkg/repo"
	"github.com/spf13/cobra"
)

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect",
		Short: "inspect a car",
		Long:  ``,
		Args:  cobra.ExactArgs(1),
		Run:   inspect,
	}
)

func inspect(c *cobra.Command, args []string) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("error opening repo: %s\n", err)
		os.Exit(1)
	}

	dir, err := repo.NewDirectory("./packages")
	if err != nil {
		fmt.Printf("error opening repo: %s\n", err)
		os.Exit(1)
	}

	L := hclog.L()

	loader, err := loader.NewLoader(L, cfg, dir)
	if err != nil {
		fmt.Printf("error creating loader: %s\n", err)
		os.Exit(1)
	}

	script, err := loader.LoadScript(args[0])
	if err != nil {
		fmt.Printf("error loading script: %s\n", err)
		os.Exit(1)
	}

	spew.Dump(script.PackageProto())

	path := "/usr/local/chell/dev"

	ar, err := archive.NewArchiver(filepath.Join(path, "store"), nil, nil)
	if err != nil {
		fmt.Printf("error loading script: %s\n", err)
		os.Exit(1)
	}

	sp, err := script.StoreName()
	if err != nil {
		fmt.Printf("error loading script: %s\n", err)
		os.Exit(1)
	}

	_, err = ar.ArchiveFromPath(ioutil.Discard, filepath.Join(path, "store", sp), "")
	if err != nil {
		fmt.Printf("error loading script: %s\n", err)
		os.Exit(1)
	}

	spew.Dump(ar.Dependencies())
}
