package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/event"
	"github.com/lab47/chell/pkg/loader"
	"github.com/lab47/chell/pkg/repo"
	"github.com/spf13/cobra"
)

var (
	sumCmd = &cobra.Command{
		Use:   "sum",
		Short: "Update sums based on used assets",
		Long:  ``,
		Args:  cobra.ExactArgs(1),
		Run:   sum,
	}
)

func sum(c *cobra.Command, args []string) {
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

	var r event.Renderer

	ctx := r.WithContext(context.Background())

	err = script.SaveSums(ctx)
	if err != nil {
		fmt.Printf("error loading script: %s\n", err)
		os.Exit(1)
	}

	sig, err := script.Signature()
	if err != nil {
		fmt.Printf("error calculate package signature: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("Signature: %s\n", sig)

	subs, err := script.Dependencies()
	if err != nil {
		fmt.Printf("error calculate package signature: %s\n", err)
		os.Exit(1)
	}

	for _, dep := range subs {
		err = dep.SaveSums(ctx)
		if err != nil {
			fmt.Printf("error loading script: %s\n", err)
			os.Exit(1)
		}

		sig, err := dep.Signature()
		if err != nil {
			fmt.Printf("error calculate package signature: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("Signature: %s\n", sig)

	}
}
