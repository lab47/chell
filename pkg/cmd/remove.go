package cmd

import (
	"fmt"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/loader"
	"github.com/lab47/chell/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	removeCmd = &cobra.Command{
		Use:   "remove",
		Short: "Remove a package",
		Long:  ``,
		Args:  cobra.ExactArgs(1),
		Run:   remove,
	}
)

func remove(c *cobra.Command, args []string) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("error loading config: %s\n", err)
		os.Exit(1)
	}

	L := hclog.L()

	loader, err := loader.NewLoader(L, cfg, cfg.Repo())
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

	prof, err := profile.OpenProfile(cfg, "")
	if err != nil {
		fmt.Printf("error opening profile: %s\n", err)
		os.Exit(1)
	}

	name, err := script.StoreName()
	if err != nil {
		fmt.Printf("error reading store name: %s\n", err)
		os.Exit(1)
	}

	err = prof.Remove(name)
	if err != nil {
		fmt.Printf("error installing into profile: %s\n", err)
		os.Exit(1)
	}
}
