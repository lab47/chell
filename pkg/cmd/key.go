package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/lab47/chell/pkg/config"
	"github.com/mr-tron/base58"
	"github.com/spf13/cobra"
)

var (
	exportKeyCmd = &cobra.Command{
		Use:   "export-key",
		Short: "Export Signing Key",
		Long:  ``,
		Args:  cobra.MaximumNArgs(1),
		Run:   exportKey,
	}
)

func exportKey(c *cobra.Command, args []string) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("error loading config: %s\n", err)
		os.Exit(1)
	}

	pub := cfg.Public()

	dir := "."

	if len(args) == 1 {
		dir = args[0]
	}

	enc := base58.Encode(pub)

	path := filepath.Join(dir, enc+".txt")

	err = ioutil.WriteFile(path, []byte(enc), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing out key: %s\n", err)
		os.Exit(1)
	}
}
