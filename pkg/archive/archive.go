package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/verification"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
	"golang.org/x/crypto/blake2b"
)

// aperature sciences ops Package
const magic = "asoP"

var ErrInvalidLink = errors.New("invalid link encountered")

type CarDependency struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Signer string `json:"signer"`
}

type CarInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`

	Repo string `json:"repo"`

	Inputs map[string]string `json:"inputs"`

	Signer    string `json:"signer"`
	Signature string `json:"signature"`

	Dependencies []*CarDependency `json:"dependencies"`
}

type Archiver struct {
	StorePath    string
	info         *CarInfo
	signer       config.EDSigner
	dependencies map[string]struct{}
}

func NewArchiver(sp string, info *CarInfo, key config.EDSigner) (*Archiver, error) {
	ar := &Archiver{
		StorePath:    sp,
		info:         info,
		dependencies: make(map[string]struct{}),
		signer:       key,
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

type nullSignOpts struct{}

func (_ nullSignOpts) HashFunc() crypto.Hash {
	return 0
}

func (ar *Archiver) ArchiveFromPath(out io.Writer, path, sig string) ([]byte, error) {
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

	h, _ := blake2b.New256(nil)

	gz := gzip.NewWriter(io.MultiWriter(out, h))
	gz.Extra = []byte(magic)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	var trbuf bytes.Buffer

	dh, _ := blake2b.New256(nil)

	for _, file := range files {
		trbuf.Reset()

		f, err := os.Open(file)
		if err != nil {
			return nil, err
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
			hdr.Format = tar.FormatPAX

			dh.Write([]byte(hdr.Name))
			dh.Write([]byte{0})

			err = tw.WriteHeader(hdr)
			if err != nil {
				return fmt.Errorf("error writing file header: %s: %w", hdr.Name, err)
			}

			if link != "" {
				return nil
			}

			var dr DepDetect
			dr.ar = ar
			dr.file = hdr.Name
			dr.prefix = []byte(ar.StorePath + "/")
			dr.buf = &trbuf

			_, err = io.Copy(io.MultiWriter(tw, dh, &dr), f)
			if err != nil {
				return fmt.Errorf("error writing file: %s: %w", hdr.Name, err)
			}

			return nil
		}()

		if err != nil {
			return nil, err
		}
	}

	signature, err := ar.signer.Sign(nil, dh.Sum(nil), nullSignOpts{})
	if err != nil {
		return nil, err
	}

	ar.info.Signature = base58.Encode(signature)

	deps, err := ar.ExpandDependencies(sig)
	if err != nil {
		return nil, err
	}

	ar.info.Dependencies = deps

	var hdr tar.Header

	hdr.Uid = 0
	hdr.Gid = 0
	hdr.Uname = ""
	hdr.Gname = ""
	hdr.AccessTime = time.Time{}
	hdr.ChangeTime = time.Time{}
	hdr.ModTime = time.Time{}
	hdr.Name = ".car-info.json"
	hdr.Format = tar.FormatPAX
	hdr.Typeflag = tar.TypeReg
	hdr.Mode = 0400

	data, err := json.MarshalIndent(ar.info, "", "  ")
	if err != nil {
		return nil, err
	}

	hdr.Size = int64(len(data))

	err = tw.WriteHeader(&hdr)
	if err != nil {
		return nil, err
	}

	_, err = tw.Write(data)
	if err != nil {
		return nil, err
	}

	err = tw.Flush()
	if err != nil {
		return nil, errors.Wrapf(err, "tar writer flush")
	}

	err = gz.Flush()
	if err != nil {
		return nil, errors.Wrapf(err, "gzip flush")
	}

	return h.Sum(nil), nil
}

func (ar *Archiver) Dependencies() []string {
	var out []string

	for k := range ar.dependencies {
		out = append(out, k)
	}

	return out
}

func (ar *Archiver) ExpandDependencies(self string) ([]*CarDependency, error) {
	f, err := os.Open(ar.StorePath)
	if err != nil {
		return nil, err
	}

	var deps []*CarDependency

	for {
		names, err := f.Readdirnames(100)
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		if len(names) == 0 {
			break
		}

		for _, name := range names {
			if idx := strings.IndexByte(name, '-'); idx != -1 {
				prefix := name[:idx]

				if prefix == self {
					continue
				}

				if _, ok := ar.dependencies[prefix]; ok {
					fi, err := os.Stat(filepath.Join(ar.StorePath, name))
					if err != nil {
						return nil, err
					}

					if !fi.IsDir() {
						continue
					}

					deps = append(deps, &CarDependency{ID: name})
				}
			}
		}
	}

	sort.Slice(deps, func(i, j int) bool {
		return deps[i].ID < deps[j].ID
	})

	return deps, nil
}

var ErrInvalidFormat = errors.New("invalid format (bad magic)")

func UnarchiveToDir(in io.Reader, path string) (*CarInfo, error) {
	h, _ := blake2b.New256(nil)

	gz, err := gzip.NewReader(io.TeeReader(in, h))
	if err != nil {
		return nil, err
	}

	if string(gz.Extra) != magic {
		return nil, ErrInvalidFormat
	}

	tr := tar.NewReader(gz)

	var ci CarInfo

	dh, _ := blake2b.New256(nil)

	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		if hdr.Name == ".car-info.json" {
			err = json.NewDecoder(tr).Decode(&ci)
			if err != nil {
				return nil, err
			}

			spew.Dump(ci)

			continue
		}

		path := filepath.Join(path, hdr.Name)
		dir := filepath.Dir(path)

		if _, err := os.Stat(dir); err != nil {
			err = os.MkdirAll(dir, 0755)
			if err != nil {
				return nil, err
			}
		}

		spew.Dump("hdr", hdr.Typeflag)

		switch hdr.Typeflag {
		case tar.TypeReg:
			dh.Write([]byte(hdr.Name))
			dh.Write([]byte{0})

			mode := hdr.FileInfo().Mode()
			f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return nil, err
			}

			io.Copy(io.MultiWriter(f, dh), tr)

			err = f.Close()
			if err != nil {
				return nil, err
			}
		case tar.TypeSymlink:
			dh.Write([]byte(hdr.Name))
			dh.Write([]byte{1})
			fmt.Fprintf(dh, hdr.Linkname)
			dh.Write([]byte{0})

			spew.Dump("read", hdr.Name, hdr.Linkname, dh.Sum(nil))

			err = os.Symlink(filepath.Join(path, hdr.Linkname), path)
			if err != nil {
				return nil, err
			}
		}
	}

	var ver verification.Verifier

	sig, err := base58.Decode(ci.Signature)
	if err != nil {
		return nil, err
	}

	err = ver.Verify(ci.Signer, sig, dh.Sum(nil))
	if err != nil {
		return nil, err
	}

	return &ci, nil
}
