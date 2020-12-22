package cmd

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/loader"
	"github.com/lab47/chell/pkg/ops"
	"github.com/lab47/chell/pkg/profile"
	"github.com/lab47/chell/pkg/runner"
	"github.com/spf13/cobra"
)

var (
	installCmd = &cobra.Command{
		Use:   "install",
		Short: "Install a package",
		Long:  ``,
		Args:  cobra.MinimumNArgs(1),
		Run:   install,
	}
)

var (
	buildOnly   bool
	outputDir   string
	force       bool
	profileName string
)

func init() {
	installCmd.PersistentFlags().BoolVarP(&buildOnly, "build-only", "B", false, "Build only")
	installCmd.PersistentFlags().StringVarP(&outputDir, "output-dir", "d", ".", "Directory to write car files when building only")
	installCmd.PersistentFlags().BoolVarP(&force, "force", "", false, "force the build")
	installCmd.PersistentFlags().StringVarP(&profileName, "profile", "p", config.DefaultProfile, "profile to install into")
}

const StoreDir = "/usr/local/chell/main/store"

func install(c *cobra.Command, args []string) {
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

	fmt.Printf("+ Adding packages to profile: %s\n", profileName)

	prof, err := profile.OpenProfile(cfg, profileName)
	if err != nil {
		log.Fatal(err)
	}

	for _, id := range toInstall.InstallOrder {
		fmt.Printf("  - %s\n", id)

		err = prof.Install(id)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func oldinstall(c *cobra.Command, args []string) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("error loading config: %s\n", err)
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

	spew.Dump(script.PackageProto())

	signerId, err := cfg.SignerId()
	if err != nil {
		fmt.Printf("error loading key: %s\n", err)
		os.Exit(1)
	}

	inst, err := runner.NewInstaller(cfg.DataDir, signerId, cfg)
	if err != nil {
		fmt.Printf("error loading script: %s\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	err = inst.MakeAvailable(ctx, script, force)
	if err != nil {
		fmt.Printf("error installing script: %s\n", err)
		os.Exit(1)
	}

	if buildOnly {
		for _, path := range inst.CreatedCars() {
			err = copyFile(filepath.Join(outputDir, filepath.Base(path)), path)
			if err != nil {
				fmt.Printf("error copying car file '%s': %s\n", path, err)
				os.Exit(1)
			}
		}
		return
	}

	prof, err := profile.OpenProfile(cfg, "")
	if err != nil {
		fmt.Printf("error opening profile: %s\n", err)
		os.Exit(1)
	}

	name, err := script.StoreName()
	if err != nil {
		fmt.Printf("error reading store name: %s\n", err)
		os.Exit(1)
	}

	err = prof.Install(name)
	if err != nil {
		fmt.Printf("error installing into profile: %s\n", err)
		os.Exit(1)
	}
}

func copyFile(dest, src string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}

	defer s.Close()

	d, err := os.Create(dest)
	if err != nil {
		return err
	}

	defer d.Close()

	io.Copy(d, s)

	return nil
}
