package cmd

import (
	"log"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/lab47/chell/pkg/ops"
	"github.com/spf13/cobra"
)

var (
	calcCmd = &cobra.Command{
		Use:   "calc",
		Short: "Calculate what to install",
		Long:  ``,
		Args:  cobra.MinimumNArgs(1),
		Run:   calc,
	}
)

func calc(c *cobra.Command, args []string) {
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

	spew.Dump(toInstall)
}
