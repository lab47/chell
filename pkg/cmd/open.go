package cmd

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lab47/chell/pkg/ops"
	"github.com/spf13/cobra"
)

var (
	openCmd = &cobra.Command{
		Use:   "open",
		Short: "Open a package",
		Long:  ``,
		Args:  cobra.MinimumNArgs(1),
		Run:   open,
	}
)

func open(c *cobra.Command, args []string) {
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

	storeDir := filepath.Join("/usr/local/chell/main/store", pkg.ID())

	if _, err := os.Stat(storeDir); err != nil {
		log.Printf("Unable to open %s: %s", args[0], err)
		return
	}

	cmd := exec.Command("bash", "-c", "exec $EDITOR "+storeDir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Run()
}
