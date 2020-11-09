package chell

import (
	"crypto/rand"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/ruby"
	archiver "github.com/mholt/archiver/v3"
	"github.com/oklog/ulid"
)

type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Rebuild int    `json:"rebuild"`
	Source  string `json:"url"`
	Sha256  string `json:"sha256"`

	Deps struct {
		Runtime []string `json:"runtime"`
	} `json:"dependencies"`

	Dependencies []*Package

	Install []string
}

type Downloader struct {
	CachePath string
}

func (d *Downloader) url(pkg *Package) string {
	if pkg.Rebuild > 0 {
		return fmt.Sprintf("https://homebrew.bintray.com/bottles/%s-%s.catalina.bottle.%d.tar.gz", pkg.Name, pkg.Version, pkg.Rebuild)
	} else {
		return fmt.Sprintf("https://homebrew.bintray.com/bottles/%s-%s.catalina.bottle.tar.gz", pkg.Name, pkg.Version)
	}
}

func (d *Downloader) Download(L hclog.Logger, pkg *Package) (string, error) {
	path := filepath.Join(d.CachePath, pkg.Sha256)

	if _, err := os.Stat(path); err == nil {
		L.Debug("package already in cache", "path", path)
		return path, err
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}

	defer f.Close()

	url := d.url(pkg)

	L.Info("downloading package", "name", pkg.Name, "url", url, "path", path)

	res, err := http.Get(url)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()

	io.Copy(f, res.Body)

	return path, nil
}

type installedPackage struct {
	pkg  *Package
	path string
}

type Installer struct {
	Downloader *Downloader
	StorePath  string

	Installed map[string]*installedPackage
}

func (i *Installer) storeName(pkg *Package) string {
	return fmt.Sprintf("%s-%s-%s", pkg.Sha256, pkg.Name, pkg.Version)
}

const PrefixPlaceholder = "@@HOMEBREW_PREFIX@@"

type replaceLib struct {
	orig, name, lib string
}

type InstalledPackage struct {
	Package   *Package
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

func (i *Installer) downloadIntoStore(L hclog.Logger, pkg *Package) error {
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

func (i *Installer) Install(L hclog.Logger, pkg *Package) (*InstalledPackage, error) {
	if i.Installed == nil {
		i.Installed = make(map[string]*installedPackage)
	}

	err := i.downloadIntoStore(L, pkg)
	if err != nil {
		return nil, err
	}

	return i.Unpack(L, pkg)
}

func (i *Installer) Unpack(L hclog.Logger, pkg *Package) (*InstalledPackage, error) {
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

	libPath := filepath.Join(root, pkg.Name, pkg.Version, "lib")

	relocate := []string{
		libPath,
		filepath.Join(root, pkg.Name, pkg.Version, "bin"),
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

	var hr HomebrewRelocator

	err = hr.Relocate(out)
	if err != nil {
		return nil, err
	}

	return out, nil
}

type Tree struct {
	Path string
}

var linkDirs = []string{"bin", "lib", "include", "man", "share"}

func (t *Tree) addDir(L hclog.Logger, spath, tpath string) error {
	files, err := ioutil.ReadDir(spath)
	if err != nil {
		return err
	}

	for _, file := range files {
		fpath := filepath.Join(spath, file.Name())

		tfpath := filepath.Join(tpath, file.Name())

		if file.IsDir() {
			err = os.MkdirAll(tfpath, 0755)
			if err != nil {
				return err
			}

			err = t.addDir(L, fpath, tfpath)
			if err != nil {
				return err
			}
		} else {
			L.Debug("populating tree", "from", fpath, "to", tfpath)

			err = os.Symlink(fpath, tfpath)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *Tree) Add(L hclog.Logger, pkg *InstalledPackage) error {
	root, err := filepath.Abs(filepath.Join(pkg.StorePath, pkg.Package.Name, pkg.Package.Version))
	if err != nil {
		return err
	}

	for _, ld := range linkDirs {
		spath := filepath.Join(root, ld)

		if _, err := os.Stat(spath); err != nil {
			continue
		}

		tpath := filepath.Join(t.Path, ld)

		if _, err := os.Stat(tpath); err != nil {
			err := os.MkdirAll(tpath, 0755)
			if err != nil {
				return err
			}
		}

		err = t.addDir(L, spath, tpath)
		if err != nil {
			return err
		}
	}

	return nil
}

type Forest struct {
	Path      string
	Installed map[string]struct{}
}

func (f *Forest) Add(L hclog.Logger, pkg *InstalledPackage) error {
	if f.Installed == nil {
		f.Installed = make(map[string]struct{})
	}

	if _, ok := f.Installed[pkg.StorePath]; ok {
		return nil
	}

	for _, sp := range pkg.Dependencies {
		err := f.Add(L, sp)
		if err != nil {
			return err
		}
	}

	root, err := filepath.Abs(f.Path)
	if err != nil {
		return err
	}

	current := filepath.Join(root, "current")
	_, err = os.Stat(current)
	if err != nil {
		u, err := ulid.New(ulid.Now(), rand.Reader)
		if err != nil {
			return err
		}

		treePath := filepath.Join(root, u.String())
		err = os.MkdirAll(treePath, 0755)
		if err != nil {
			return err
		}

		L.Debug("created new tree in the forest", "path", treePath)

		err = os.Symlink(treePath, current)
		if err != nil {
			return err
		}
	}

	var t Tree
	t.Path = current

	return t.Add(L, pkg)
}

const CoreTap = "/usr/local/Homebrew/Library/Taps/homebrew/homebrew-core/Formula"

type InstallOptions struct {
	Debug       bool
	CachePath   string
	StorePath   string
	ForestPath  string
	TempPath    string
	PackagePath string
}

func DefaultInstallOptions() (InstallOptions, error) {
	var opts InstallOptions

	u, err := user.Current()
	if err != nil {
		return opts, err
	}

	root := filepath.Join(u.HomeDir, ".chell")

	return RootedInstallOptions(root)
}

func RootedInstallOptions(root string) (InstallOptions, error) {
	var opts InstallOptions

	opts.CachePath = filepath.Join(root, "cache")
	opts.StorePath = filepath.Join(root, "store")
	opts.ForestPath = filepath.Join(root, "forest")
	opts.TempPath = filepath.Join(root, "tmp")
	opts.PackagePath = filepath.Join(root, "packages")

	return opts, nil
}

func RubyInstall(L hclog.Logger, name string, opts InstallOptions) error {
	path := filepath.Join(CoreTap, name+".rb")

	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("unknown package: %s", name)
	}

	pkgs := map[string]*Package{}

	err := os.MkdirAll(opts.TempPath, 0755)
	if err != nil {
		return err
	}

	l, err := ruby.NewLoader(L, opts.TempPath)
	if err != nil {
		return err
	}

	err = l.Load(path, &pkgs)
	if err != nil {
		return err
	}

	pkg, ok := pkgs[name]
	if !ok {
		log.Fatalf("missing %s", name)
	}

	for _, name := range pkg.Deps.Runtime {
		sub, ok := pkgs[name]
		if !ok {
			log.Fatalf("missing dependency: %s", name)
		}

		pkg.Dependencies = append(pkg.Dependencies, sub)
	}

	err = os.MkdirAll(opts.CachePath, 0755)
	if err != nil {
		return err
	}

	err = os.MkdirAll(opts.StorePath, 0755)
	if err != nil {
		return err
	}

	err = os.MkdirAll(opts.ForestPath, 0755)
	if err != nil {
		return err
	}

	var d Downloader
	d.CachePath = opts.CachePath

	var i Installer
	i.StorePath = opts.StorePath
	i.Downloader = &d

	var f Forest
	f.Path = opts.ForestPath

	ip, err := i.Install(L, pkg)
	if err != nil {
		return err
	}

	err = f.Add(L, ip)
	if err != nil {
		return err
	}

	return nil
}
