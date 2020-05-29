package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// aperature sciences ops Package
const magic = "asoP"

var ErrInvalidLink = errors.New("invalid link encountered")

type Archiver struct {
	StorePath    string
	dependencies map[string]struct{}
}

func NewArchiver(sp string) (*Archiver, error) {

	ar := &Archiver{
		StorePath:    sp,
		dependencies: make(map[string]struct{}),
	}

	return ar, nil
}

var (
	validHashChars map[byte]struct{}
	placeholder    = []byte("@@CHELL_STORE@@")
)

const hashAlphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func init() {
	validHashChars = map[byte]struct{}{}

	for _, c := range hashAlphabet {
		validHashChars[byte(c)] = struct{}{}
	}
}

func (ar *Archiver) extractDependencies(file string, src []byte) {
	sp := []byte(ar.StorePath)

	for {
		idx := bytes.Index(src, sp)
		if idx == -1 {
			return
		}

		// scan the bit right after the store path to find the hash seen
		var hash string

		start := idx + len(sp) + 1

		var j int

		for j = start; j < len(src); j++ {
			_, found := validHashChars[src[j]]
			if !found {
				hash = string(src[start:j])
				break
			}
		}

		if hash != "" {
			ar.dependencies[hash] = struct{}{}
		}

		fmt.Fprintf(os.Stderr, "discovered reference: %s => %s\n", file, string(src[idx:j]))

		src = src[j:]
	}
}

func (ar *Archiver) ArchiveFromPath(out io.Writer, path string) error {
	var files []string

	filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		switch info.Mode() & os.ModeType {
		case 0, os.ModeSymlink:
			files = append(files, path)
		}

		return nil
	})

	sort.Strings(files)

	gz := gzip.NewWriter(out)
	gz.Extra = []byte(magic)
	defer gz.Close()

	tw := tar.NewWriter(out)
	defer tw.Close()

	var trbuf bytes.Buffer

	for _, file := range files {
		trbuf.Reset()

		f, err := os.Open(file)
		if err != nil {
			return err
		}

		err = func() error {
			defer f.Close()

			fi, err := f.Stat()
			if err != nil {
				return err
			}

			var link string

			if fi.Mode()&os.ModeSymlink != 0 {
				link, err = os.Readlink(file)
				if err != nil {
					return err
				}

				if !strings.HasPrefix(link, path) {
					return errors.Wrapf(ErrInvalidLink, "link points outside of root dir: %s", link)
				}

				link = link[len(path)+1:]
			}

			hdr, err := tar.FileInfoHeader(fi, link)
			if err != nil {
				return err
			}

			hdr.Uid = 0
			hdr.Gid = 0
			hdr.Uname = ""
			hdr.Gname = ""
			hdr.AccessTime = time.Time{}
			hdr.ChangeTime = time.Time{}
			hdr.ModTime = time.Time{}
			hdr.Name = file[len(path)+1:]
			hdr.Format = tar.FormatUSTAR

			tw.WriteHeader(hdr)

			if link != "" {
				return nil
			}

			var dr DepDetect
			dr.ar = ar
			dr.file = hdr.Name
			dr.buf = &trbuf

			_, err = io.Copy(tw, io.TeeReader(f, &dr))
			return err
		}()

		if err != nil {
			return err
		}
	}

	return nil
}

func (ar *Archiver) Dependencies() []string {
	var out []string

	for k := range ar.dependencies {
		out = append(out, k)
	}

	return out
}

var ErrInvalidFormat = errors.New("invalid format (bad magic)")

func UnarchiveToDir(in io.Reader, path string) error {
	gz, err := gzip.NewReader(in)
	if err != nil {
		return err
	}

	if string(gz.Extra) != magic {
		return ErrInvalidFormat
	}

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err != io.EOF {
			break
		}

		path := filepath.Join(path, hdr.Name)
		dir := filepath.Base(path)

		if _, err := os.Stat(dir); err != nil {
			err = os.MkdirAll(dir, 0755)
			if err != nil {
				return err
			}
		}

		switch hdr.Typeflag {
		case tar.TypeReg:
			f, err := os.Create(path)
			if err != nil {
				return err
			}

			io.Copy(f, tr)

			err = f.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			err = os.Symlink(filepath.Join(path, hdr.Linkname), path)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
