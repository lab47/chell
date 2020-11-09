package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/loader"
	"github.com/lab47/chell/pkg/profile"
	"github.com/lab47/chell/pkg/runner"
	"github.com/spf13/cobra"
)

var (
	installCmd = &cobra.Command{
		Use:   "install",
		Short: "Install a package",
		Long:  ``,
		Args:  cobra.ExactArgs(1),
		Run:   install,
	}
)

var (
	buildOnly bool
	force     bool
)

func init() {
	installCmd.PersistentFlags().BoolVarP(&buildOnly, "build-only", "B", false, "Build only")
	installCmd.PersistentFlags().BoolVarP(&force, "force", "", false, "force the build")
}

func install(c *cobra.Command, args []string) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("error loading config: %s\n", err)
		os.Exit(1)
	}

	/*
		dir, err := repo.NewDirectory("./packages")
		if err != nil {
			fmt.Printf("error opening repo: %s\n", err)
			os.Exit(1)
		}
	*/

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

	signerId, err := cfg.SignerId()
	if err != nil {
		fmt.Printf("error loading key: %s\n", err)
		os.Exit(1)
	}

	inst, err := runner.NewInstaller(cfg.DataDir, signerId, cfg)
	if err != nil {
		fmt.Printf("error loading script: %s\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	err = inst.MakeAvailable(ctx, script, force)
	if err != nil {
		fmt.Printf("error installing script: %s\n", err)
		os.Exit(1)
	}

	if buildOnly {
		return
	}

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

	err = prof.Install(name)
	if err != nil {
		fmt.Printf("error installing into profile: %s\n", err)
		os.Exit(1)
	}
}