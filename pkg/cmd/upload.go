package cmd

import (
	"fmt"
	"log"
	"strings"

	"github.com/lab47/chell/pkg/ops"
	"github.com/spf13/cobra"
)

var (
	uploadCmd = &cobra.Command{
		Use:   "upload",
		Short: "Upload cars",
		Long:  ``,
		Args:  cobra.MinimumNArgs(1),
		Run:   upload,
	}
)

var (
	uploadInputDir string
	uploadBucket   string
	uploadAll      bool
)

func init() {
	uploadCmd.PersistentFlags().StringVarP(&uploadInputDir, "input-dir", "d", ".", "Directory to read car files")
	uploadCmd.PersistentFlags().StringVarP(&uploadBucket, "bucket", "b", "", "S3 bucket to store files into")
	uploadCmd.PersistentFlags().BoolVarP(&uploadAll, "all", "A", false, "Upload all dependencies, not just runtime")
}

func upload(c *cobra.Command, args []string) {
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

	fmt.Printf("+ Uploading packages from %s to %s...\n", uploadInputDir, uploadBucket)

	cu, err := o.CarUploadS3(uploadBucket, uploadInputDir)
	if err != nil {
		log.Fatal(err)
	}

	if !uploadAll {
		err = cu.Upload(pkg.ID())
		if err != nil {
			log.Fatal(err)
		}

		return
	}

	pci := o.PackageCalcInstall()

	toInstall, err := pci.Calculate(pkg)
	if err != nil {
		log.Fatal(err)
	}

	err = cu.UploadExplicit(toInstall.InstallOrder)
	if err != nil {
		log.Fatal(err)
	}
}
