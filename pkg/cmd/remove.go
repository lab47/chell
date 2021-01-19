package cmd

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/lab47/chell/pkg/ops"
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

	os.RemoveAll(filepath.Join(cfg.StorePath(), pkg.ID()))
}
