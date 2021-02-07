package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
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
		Args:  cobra.MinimumNArgs(0),
		Run:   shell,
	}
)

var shellFlags struct {
	printEnv bool
}

func init() {
	shellCmd.PersistentFlags().BoolVarP(&shellFlags.printEnv, "print-env", "E", false, "print the environment that would be added")
}

func shell(c *cobra.Command, args []string) {
	o, cfg, err := loadAPI()
	if err != nil {
		log.Fatal(err)
	}

	pl := o.ProjectLoad()

	r, err := os.Open("project.chell")
	if err != nil {
		log.Fatal(err)
	}

	proj, err := pl.LoadScript(r,
		ops.WithConstraints(cfg.Constraints()),
	)
	if err != nil {
		log.Fatal(err)
	}

	pci := o.PackageCalcInstall()

	toInstall, err := pci.CalculateSet(proj.ToInstall)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan os.Signal, 1)

	go func() {
		<-ch
		cancel()
	}()

	signal.Notify(ch, os.Interrupt, os.Kill, syscall.SIGQUIT)

	ui := ops.GetUI(ctx)
	ui.InstallPrologue(cfg)

	buildDir, err := ioutil.TempDir("", "chell-build")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(buildDir)

	ienv := &ops.InstallEnv{
		BuildDir:   buildDir,
		StoreDir:   StoreDir,
		StartShell: dev,
	}

	install := o.PackagesInstall(ienv)

	err = install.Install(ctx, toInstall)
	if err != nil {
		log.Print(err)
		return
	}

	var path []string

	deps, err := o.ScriptAllDeps().EvalDeps(proj.ToInstall)
	if err != nil {
		log.Panic(err)
		return
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

	if shellFlags.printEnv {
		fmt.Printf("PATH=" + curPath)
		os.Exit(0)
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
