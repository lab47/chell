package cmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/lab47/chell/pkg/ops"
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
	o, cfg, err := loadAPI()
	if err != nil {
		log.Fatal(err)
	}

	sl := o.ScriptLoad()

	scriptArgs := make(map[string]string)

	for _, a := range args[1:] {
		idx := strings.IndexByte(a, '=')
		if idx > -1 {
			scriptArgs[a[:idx]] = a[idx+1:]
		}
	}

	ns, name := parseName(args[0])

	pkg, err := sl.Load(
		name,
		ops.WithNamespace(ns),
		ops.WithArgs(scriptArgs),
		ops.WithConstraints(cfg.Constraints()),
	)
	if err != nil {
		log.Fatal(err)
	}

	var path []string

	deps, err := o.ScriptAllDeps().RuntimeDeps(pkg)
	if err != nil {
		log.Panic(err)
		return
	}

	binPath := filepath.Join(cfg.StorePath(), pkg.ID(), "bin")
	if _, err := os.Stat(binPath); err == nil {
		path = append(path, binPath)
	}

	for _, dep := range deps {
		binPath := filepath.Join(cfg.StorePath(), dep.ID(), "bin")
		if _, err := os.Stat(binPath); err == nil {
			path = append(path, binPath)
		}
	}

	pkgPath := strings.Join(path, ":")

	curPath := os.Getenv("PATH")
	if curPath != "" {
		curPath = pkgPath + ":" + curPath
	} else {
		curPath = pkgPath
	}

	os.Setenv("PATH", curPath)

	args = args[1:]

	if len(args) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}

		args = []string{shell}
	}

	exec, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Printf("Unable to find command: %s (%s)\n", args[0], err)
		os.Exit(1)
	}

	err = syscall.Exec(exec, args, os.Environ())
	fmt.Printf("error execing: %s\n", err)
	os.Exit(1)
}
