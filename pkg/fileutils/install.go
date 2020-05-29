package fileutils

import (
	"io"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
)

type Install struct {
	L       hclog.Logger
	Pattern string
	Dest    string
	Linked  bool
}

func (i *Install) Install() error {
	if i.L == nil {
		i.L = hclog.L()
	}

	entries, err := filepath.Glob(i.Pattern)
	if err != nil {
		return err
	}

	baseDir := filepath.Dir(i.Pattern)

	if _, err := os.Stat(i.Dest); err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(i.Dest, 0755)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	for _, ent := range entries {
		rel, err := filepath.Rel(baseDir, ent)
		if err != nil {
			return err
		}

		target := filepath.Join(i.Dest, rel)

		if i.Linked {
			i.L.Debug("symlink", "old", ent, "new", target)

			err = os.Symlink(ent, target)
		} else {
			err = i.copyEntry(ent, target)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (i *Install) copyEntry(from, to string) error {
	i.L.Debug("copy entry", "from", from, "to", to)

	f, err := os.Open(from)
	if err != nil {
		return err
	}

	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	switch fi.Mode() & os.ModeType {
	case 0: // regular file
		tg, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
		if err != nil {
			return err
		}

		defer tg.Close()

		_, err = io.Copy(tg, f)
		if err != nil {
			if err != io.EOF {
				return err
			}
		}

		return nil
	case os.ModeDir:
		err = os.Mkdir(to, fi.Mode().Perm())
		if err != nil {
			return err
		}

		for {
			names, err := f.Readdirnames(50)
			if err != nil {
				if err == io.EOF {
					break
				}

				return err
			}

			for _, name := range names {
				err = i.copyEntry(filepath.Join(from, name), filepath.Join(to, name))
				if err != nil {
					return err
				}
			}
		}

	case os.ModeSymlink:
		link, err := os.Readlink(from)
		if err != nil {
			return err
		}

		return os.Symlink(link, to)
	}

	return nil
}