package chell

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

type InstallReceipt struct {
	ChangedFiles []string `json:"changed_files"`
}

type HomebrewRelocator struct{}

const ReceiptJson = "INSTALL_RECEIPT.json"

func (h *HomebrewRelocator) Relocate(pkg *InstalledPackage) error {
	sp, err := filepath.Abs(pkg.StorePath)
	if err != nil {
		return err
	}

	root, err := filepath.Abs(filepath.Join(sp, pkg.Package.Name, pkg.Package.Version))

	path := filepath.Join(root, ReceiptJson)

	f, err := os.Open(path)
	if err != nil {
		return err
	}

	defer f.Close()

	var rec InstallReceipt

	err = json.NewDecoder(f).Decode(&rec)
	if err != nil {
		return err
	}

	replacer := strings.NewReplacer(
		"@@HOMEBREW_PREFIX@@", root,
		"@@HOMEBREW_CELLAR@@", sp,
		"@@HOMEBREW_REPOSITORY@@", "/usr/local/Homebrew",
	)

	for _, file := range rec.ChangedFiles {
		fpath := filepath.Join(root, file)

		fi, err := os.Stat(fpath)
		if err != nil {
			return err
		}

		data, err := ioutil.ReadFile(fpath)
		if err != nil {
			return err
		}

		data = []byte(replacer.Replace(string(data)))

		err = os.Chmod(fpath, fi.Mode().Perm()|0200)
		if err != nil {
			return err
		}

		err = ioutil.WriteFile(fpath, data, fi.Mode().Perm())
		if err != nil {
			return err
		}

		err = os.Chmod(fpath, fi.Mode().Perm())
		if err != nil {
			return err
		}
	}

	return nil
}
