package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/loader"
	"github.com/spf13/cobra"
)

var (
	shellCmd = &cobra.Command{
		Use:   "shell",
		Short: "Run a shell setup for the given package",
		Long:  ``,
		Args:  cobra.MinimumNArgs(1),
		Run:   shell,
	}
)

func shell(c *cobra.Command, args []string) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("error opening repo: %s\n", err)
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

	env, err := script.Env(filepath.Join(cfg.DataDir, "store"))
	if err != nil {
		fmt.Printf("error getting env: %s\n", err)
		os.Exit(1)
	}

	for k, v := range env {
		cur, ok := os.LookupEnv(k)
		if ok {
			cur = v + string(filepath.ListSeparator) + cur
		} else {
			cur = v
		}

		err = os.Setenv(k, cur)
		if err != nil {
			fmt.Printf("error setting env: %s\n", err)
			os.Exit(1)
		}
	}

	args = args[1:]

	if len(args) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}

		args = []string{shell}
	}

	path, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Printf("Unable to find command: %s (%s)\n", args[0], err)
		os.Exit(1)
	}

	err = syscall.Exec(path, args, os.Environ())
	fmt.Printf("error execing: %s\n", err)
	os.Exit(1)
}
