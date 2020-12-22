package ops

import (
	"context"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

var ErrInstallError = errors.New("installation error")

type PackagesInstall struct {
	ienv *InstallEnv

	Installed []string
	Failed    string
}

func (p *PackagesInstall) Install(ctx context.Context, toInstall *PackagesToInstall) error {
	for _, id := range toInstall.InstallOrder {
		storeDir := filepath.Join(p.ienv.StoreDir, id)
		if _, err := os.Stat(storeDir); err == nil {
			continue
		}

		fn, ok := toInstall.Installers[id]
		if !ok {
			return errors.Wrapf(ErrInstallError, "missing installer for %s", id)
		}

		err := fn.Install(ctx, p.ienv)
		if err != nil {
			p.Failed = id
			os.RemoveAll(storeDir)
			return err
		}

		p.Installed = append(p.Installed, id)
	}

	return nil
}
