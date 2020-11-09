package cmd

import (
	"fmt"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/gc"
	"github.com/spf13/cobra"
)

var (
	gcCmd = &cobra.Command{
		Use:   "gc",
		Short: "Garbage Collect Packages",
		Long:  ``,
		Args:  cobra.ExactArgs(0),
		Run:   runGC,
	}
)

func runGC(c *cobra.Command, args []string) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("error loading config: %s\n", err)
		os.Exit(1)
	}

	col, err := gc.NewCollector(cfg.DataDir)
	if err != nil {
		fmt.Printf("error creating collector: %s\n", err)
		os.Exit(1)
	}

	sr, err := col.SweepAndRemove()
	if err != nil {
		fmt.Printf("error marking packages: %s\n", err)
		os.Exit(1)
	}

	spew.Dump(sr)
}
