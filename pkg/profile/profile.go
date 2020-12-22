package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lab47/chell/pkg/config"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
)

type Profile struct {
	cfg  *config.Config
	path string
}

func hashString(str string) string {
	h, _ := blake2b.New256(nil)
	h.Write([]byte(str))
	return base58.Encode(h.Sum(nil))
}

func OpenProfile(cfg *config.Config, name string) (*Profile, error) {
	if name == "" {
		name = cfg.Profile
	}

	path := filepath.Join(cfg.ProfilesPath, name)

	if _, err := os.Stat(path); err != nil {
		err = os.MkdirAll(path, 0755)
		if err != nil {
			return nil, err
		}
	}

	return &Profile{
		cfg:  cfg,
		path: path,
	}, nil
}

func (p *Profile) Install(name string) error {
	root := filepath.Join(p.cfg.StorePath(), name)

	fi, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("unknown package: %s", name)
	}

	if !fi.IsDir() {
		return fmt.Errorf("corrupt store detected (not a dir: %s)", root)
	}

	pkgDir := filepath.Join(p.path, ".chell-packages")

	err = os.MkdirAll(pkgDir, 0755)
	if err != nil {
		return err
	}

	trackLink := filepath.Join(pkgDir, name)

	// It's already setup, no need.
	if _, err := os.Stat(trackLink); err == nil {
		return nil
	}

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if rel == "" {
			return nil
		}

		target := filepath.Join(p.path, rel)

		fi, err := os.Lstat(target)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}

			err = os.Symlink(path, target)
			if err != nil {
				return err
			}

			if info.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if !info.IsDir() {
			lt, err := os.Readlink(target)
			if err != nil || lt != path {
				fmt.Printf("skipping duplicate entries for %s", target)
			}

			return nil
		}

		if fi.IsDir() {
			return nil
		}

		lfi, err := os.Stat(target)
		if err != nil {
			return err
		}

		if !lfi.IsDir() {
			return fmt.Errorf("unable to merge file and dir at path: %s", target)
		}

		odir, err := os.Readlink(target)
		if err != nil {
			return err
		}

		f, err := os.Open(target)
		if err != nil {
			return err
		}

		defer f.Close()

		names, err := f.Readdirnames(-1)
		if err != nil {
			return err
		}

		os.Remove(target)

		err = os.Mkdir(target, 0755)
		if err != nil {
			return err
		}

		for _, name := range names {
			err = os.Symlink(filepath.Join(odir, name), filepath.Join(target, name))
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return err
	}

	err = os.Symlink(root, trackLink)
	if err != nil {
		return err
	}

	os.Symlink(
		filepath.Join(p.path, ".chell-packages"),
		filepath.Join(p.cfg.RootsPath(), hashString(p.path)))

	return nil
}

// readDirNames reads the directory named by dirname and returns
// a sorted list of directory entries.
func readDirNames(dirname string) ([]string, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	sort.Strings(names)
	return names, nil
}

// walk recursively descends path, calling walkFn.
func walkDF(path string) (bool, error) {
	names, err := readDirNames(path)
	if err != nil {
		return false, err
	}

	empty := true

	for _, name := range names {
		filename := filepath.Join(path, name)
		fileInfo, err := os.Lstat(filename)
		if err != nil {
			return false, err
		}

		if !fileInfo.IsDir() {
			empty = false
			continue
		}

		mt, err := walkDF(filename)
		if err != nil {
			return false, err
		}

		if mt {
			err = os.Remove(filename)
			if err != nil {
				return false, err
			}
		} else {
			empty = false
		}
	}

	return empty, nil
}

func (p *Profile) Remove(name string) error {
	prefix := filepath.Join(p.cfg.StorePath(), name)

	err := filepath.Walk(p.path, func(path string, info os.FileInfo, err error) error {
		if info.Mode()&os.ModeType != os.ModeSymlink {
			return nil
		}

		target, err := os.Readlink(path)
		if err != nil {
			return err
		}

		if strings.HasPrefix(target, prefix) {
			return os.Remove(path)
		}

		return nil
	})

	if err != nil {
		return err
	}

	_, err = walkDF(p.path)
	return err
}
