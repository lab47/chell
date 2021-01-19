package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"
	"text/tabwriter"

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

	pci := o.PackageCalcInstall()

	toInstall, err := pci.Calculate(pkg)
	if err != nil {
		log.Fatal(err)
	}

	tr := tabwriter.NewWriter(os.Stdout, 4, 2, 2, ' ', 0)

	defer tr.Flush()

	fmt.Fprintf(tr, "ID\tTYPE\tREPO\n")

	for _, id := range toInstall.InstallOrder {
		if scr, ok := toInstall.Scripts[id]; ok {
			fmt.Fprintf(tr, "%s\tscript\t%s\n", id, scr.Repo())
		} else if toInstall.Installed[id] {
			fmt.Fprintf(tr, "%s\tinstalled\t\n", id)
		} else {
			fmt.Fprintf(tr, "%s\tcar\t\n", id)
		}
	}

}

var (
	calcLibsCmd = &cobra.Command{
		Use:   "calc-libs",
		Short: "Calculate what to libraries are used",
		Long:  ``,
		Args:  cobra.MinimumNArgs(1),
		Run:   calcLibs,
	}
)

func calcLibs(c *cobra.Command, args []string) {
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

	libs, err := o.PackageDetectLibs().Detect(pkg.ID())
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Chell libs:\n")
	for _, lib := range libs.ChellLibs {
		fmt.Printf("- %s\n", lib)
	}

	fmt.Printf("System libs:\n")
	for _, lib := range libs.SystemLibs {
		fmt.Printf("- %s\n", lib)
	}
}
