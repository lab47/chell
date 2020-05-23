package installer

import (
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/evanphx/chell/pkg/chell"
	"github.com/hashicorp/go-hclog"
	"github.com/mholt/archiver/v3"
)

type installedPackage struct {
	pkg  *chell.Package
	path string
}

type Installer struct {
	Downloader *chell.Downloader
	StorePath  string

	Installed map[string]*installedPackage
}

func (i *Installer) storeName(pkg *chell.Package) string {
	return fmt.Sprintf("%s-%s-%s", pkg.Sha256, pkg.Name, pkg.Version)
}

const PrefixPlaceholder = "@@HOMEBREW_PREFIX@@"

type replaceLib struct {
	orig, name, lib string
}

type InstalledPackage struct {
	Package   *chell.Package
	StorePath string

	Dependencies []*InstalledPackage
}

const (
	Magic32  uint32 = 0xfeedface
	Magic64  uint32 = 0xfeedfacf
	MagicFat uint32 = 0xcafebabe
)

func isDyLib(path string) bool {
	r, err := os.Open(path)
	if err != nil {
		return false
	}

	defer r.Close()

	// Read and decode Mach magic to determine byte order, size.
	// Magic32 and Magic64 differ only in the bottom bit.
	var ident [4]byte
	if _, err := r.ReadAt(ident[0:], 0); err != nil {
		return false
	}

	be := binary.BigEndian.Uint32(ident[0:])
	le := binary.LittleEndian.Uint32(ident[0:])
	switch Magic32 &^ 1 {
	case be &^ 1:
		return true
	case le &^ 1:
		return true
	}

	return false
}

func (i *Installer) downloadIntoStore(L hclog.Logger, pkg *chell.Package) error {
	root := filepath.Join(i.StorePath, i.storeName(pkg))

	i.Installed[pkg.Name] = &installedPackage{pkg: pkg, path: root}

	if _, err := os.Stat(root); err == nil {
		return nil
	}

	for _, dep := range pkg.Dependencies {
		err := i.downloadIntoStore(L, dep)
		if err != nil {
			return err
		}
	}

	path, err := i.Downloader.Download(L, pkg)
	if err != nil {
		return err
	}

	if _, err := os.Stat(root); err != nil {
		L.Info("unpackaging package", "name", pkg.Name, "cache-path", path, "store-path", root)

		err = archiver.DefaultTarGz.Unarchive(path, root)
		if err != nil {
			return err
		}
	}

	i.Installed[pkg.Name] = &installedPackage{pkg: pkg, path: root}

	return err
}

func (i *Installer) Install(L hclog.Logger, pkg *chell.Package) (*InstalledPackage, error) {
	if i.Installed == nil {
		i.Installed = make(map[string]*installedPackage)
	}

	err := i.downloadIntoStore(L, pkg)
	if err != nil {
		return nil, err
	}

	return i.Unpack(L, pkg)
}

func (i *Installer) Unpack(L hclog.Logger, pkg *chell.Package) (*InstalledPackage, error) {
	root := filepath.Join(i.StorePath, i.storeName(pkg))

	out := &InstalledPackage{
		Package:   pkg,
		StorePath: root,
	}

	for _, dep := range pkg.Dependencies {
		ip, err := i.Unpack(L, dep)
		if err != nil {
			return nil, err
		}

		out.Dependencies = append(out.Dependencies, ip)
	}

	libPath := filepath.Join(root, "lib")

	relocate := []string{
		libPath,
		filepath.Join(root, "bin"),
	}

	libFiles, err := ioutil.ReadDir(libPath)
	if err == nil {
		for _, file := range libFiles {
			fpath := filepath.Join(libPath, file.Name())

			if !isDyLib(fpath) {
				continue
			}

			fi, err := os.Lstat(fpath)
			if err != nil {
				return nil, err
			}

			if !fi.Mode().IsRegular() {
				continue
			}

			L.Debug("patching dylib id", "lib", fpath)

			fpath, err = filepath.Abs(fpath)
			if err != nil {
				return nil, err
			}

			perm := fi.Mode().Perm()

			err = os.Chmod(fpath, perm|0200)
			if err != nil {
				return nil, err
			}

			cmd := exec.Command("install_name_tool", "-id", fpath, fpath)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			err = cmd.Run()
			if err != nil {
				return nil, err
			}
		}
	}

	for _, dir := range relocate {
		files, err := ioutil.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, file := range files {
			fpath := filepath.Join(dir, file.Name())

			mofile, err := macho.Open(fpath)
			if err != nil {
				continue
			}

			var replaceLibs []*replaceLib

			for _, load := range mofile.Loads {
				if dylib, ok := load.(*macho.Dylib); ok {
					if strings.Contains(dylib.Name, PrefixPlaceholder) {
						parts := strings.Split(dylib.Name, "/")
						replaceLibs = append(replaceLibs, &replaceLib{
							orig: dylib.Name,
							name: parts[2],
							lib:  filepath.Join(parts[3:]...),
						})
					}
				}
			}

			if len(replaceLibs) > 0 {
				L.Debug("patching mach-o dependent libraries", "path", fpath)
			}

			for _, lib := range replaceLibs {
				ipkg := i.Installed[lib.name]

				if ipkg == nil {
					spew.Dump(lib.name)
					panic("huh?")
				}

				newPath, err := filepath.Abs(filepath.Join(ipkg.path, ipkg.pkg.Name, ipkg.pkg.Version, lib.lib))
				if err != nil {
					return nil, err
				}

				newPath = filepath.Clean(newPath)

				L.Debug("patching library", "file", fpath, "orig", lib.orig, "new", newPath)

				fi, err := os.Lstat(fpath)
				if err != nil {
					return nil, err
				}

				perm := fi.Mode().Perm()

				err = os.Chmod(fpath, perm|0200)
				if err != nil {
					return nil, err
				}

				cmd := exec.Command("install_name_tool", "-change", lib.orig, newPath, fpath)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr

				err = cmd.Run()
				if err != nil {
					return nil, err
				}
			}
		}
	}

	/*
		var hr chell.HomebrewRelocator

		err = hr.Relocate(out)
		if err != nil {
			return nil, err
		}
	*/

	return out, nil
}
