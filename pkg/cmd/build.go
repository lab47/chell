package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/lab47/chell/pkg/ops"
	"github.com/spf13/cobra"
)

var (
	buildCmd = &cobra.Command{
		Use:   "build",
		Short: "Build a package and it's depnedencies to car files",
		Long:  ``,
		Args:  cobra.MinimumNArgs(1),
		Run:   build,
	}
)

var (
	buildOutputDir string
)

func init() {
	buildCmd.PersistentFlags().StringVarP(&buildOutputDir, "output-dir", "d", ".", "Directory to write car files when building only")
}

func build(c *cobra.Command, args []string) {
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

	pkg, err := sl.Load(
		args[0],
		ops.WithArgs(scriptArgs),
		ops.WithConstraints(cfg.Constraints()),
	)
	if err != nil {
		log.Fatal(err)
	}

	pci := o.PackageCalcInstall()

	toInstall, err := pci.Calculate(pkg)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	buildDir, err := ioutil.TempDir("", "chell-build")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(buildDir)

	ienv := &ops.InstallEnv{
		BuildDir: buildDir,
		StoreDir: StoreDir,
	}

	err = os.MkdirAll(ienv.StoreDir, 0755)
	if err != nil {
		log.Print(err)
		return
	}

	install := o.PackagesInstall(ienv)

	err = install.Install(ctx, toInstall)
	if err != nil {
		log.Print(err)
		return
	}

	fmt.Printf("+ Write packages to %s\n", buildOutputDir)

	stc := o.StoreToCar(buildOutputDir)

	for _, id := range toInstall.InstallOrder {
		pkg, ok := toInstall.Scripts[id]
		if !ok {
			continue
		}

		fmt.Printf("  - %s\n", id)

		err = stc.Pack(ctx, pkg)
		if err != nil {
			log.Fatal(err)
		}
	}
}
