package ops

import (
	"context"
	"fmt"

	"github.com/lab47/chell/pkg/config"
	"github.com/mr-tron/base58"
)

type UI struct {
}

func (u *UI) RunScript(pkg *ScriptPackage) error {
	fmt.Printf("Compiling %s/%s:%s (%s)...\n", pkg.Repo(), pkg.ID(), pkg.cs.Version, pkg.ID())
	return nil
}

func (u *UI) InstallCar(url string) error {
	fmt.Printf("Installing car %s\n", url)
	return nil
}

func (u *UI) DownloadInput(url, ht string, hash []byte) error {
	fmt.Printf("Downloading %s (%s:%s)\n", url, ht, base58.Encode(hash))
	return nil
}

func (u *UI) InstallPrologue(cfg *config.Config) error {
	constraints := cfg.Constraints()

	var keys []string

	for k := range constraints {
		keys = append(keys, k)
	}

	fmt.Printf("Constraints:\n")

	for _, k := range keys {
		fmt.Printf("%s: %s\n", k, constraints[k])
	}

	return nil
}

type uiMarker struct{}

func GetUI(ctx context.Context) *UI {
	v := ctx.Value(uiMarker{})
	if v == nil {
		return &UI{}
	}

	return v.(*UI)
}