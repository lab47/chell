package archive

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/transform"

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

func isTextFile(br io.RuneReader) (bool, error) {
	// Use the strategy employed by most, ie, just test the first N bytes
	// of a file. We're going to use 4096 since it's a common golang
	// buffer size anyway
	for i := 0; i < 4096; i++ {
		r, _, err := br.ReadRune()
		if err != nil {
			if err == io.EOF {
				break
			}

			return false, err
		}

		if r == utf8.RuneError {
			return false, nil
		}
	}

	return true, nil
}

// Transform writes to dst the transformed bytes read from src, and
// returns the number of dst bytes written and src bytes read. The
// atEOF argument tells whether src represents the last bytes of the
// input.
//
// Callers should always process the nDst bytes produced and account
// for the nSrc bytes consumed before considering the error err.
//
// A nil error means that all of the transformed bytes (whether freshly
// transformed from src or left over from previous Transform calls)
// were written to dst. A nil error can be returned regardless of
// whether atEOF is true. If err is nil then nSrc must equal len(src);
// the converse is not necessarily true.
//
// ErrShortDst means that dst was too short to receive all of the
// transformed bytes. ErrShortSrc means that src had insufficient data
// to complete the transformation. If both conditions apply, then
// either error may be returned. Other than the error conditions listed
// here, implementations are free to report other errors that arise.
func (n *Archiver) Transform(dst []byte, src []byte, atEOF bool) (nDst int, nSrc int, err error) {
	sp := []byte(n.StorePath)

	for len(src) > 0 {
		idx := bytes.Index(src, sp)
		if idx == -1 {
			n := copy(dst, src)
			nDst += n
			nSrc += n
			err = nil
			return
		}

		// The easy case

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
			n.dependencies[hash] = struct{}{}
		} else if j == len(src) {
			fmt.Fprintf(os.Stderr, "boundary case\n")
			// ok, we didn't have enough to read a hash so say we need more src
			n := copy(dst, src[:idx])
			nSrc += n
			nDst += n
			err = transform.ErrShortSrc
			return
		}

		// Ok, we detected a valid storePath, let's filter it
		m := copy(dst, src[:idx])
		repStart := m
		m += copy(dst[m:], placeholder)

		fmt.Fprintf(os.Stderr, "transformed `%s` => `%s`\n", src[idx:idx+len(sp)], dst[repStart:m])

		section := idx + len(sp)
		nDst += m
		nSrc += section

		fmt.Fprintf(os.Stderr, "tcopy: %d / %d\n", nSrc, nDst)

		src = src[section:]
		continue
		/*
			} else if len(src) < len(sp) {
				// see if there is maybe the start of the store path
				idx := bytes.IndexByte(src, sp[0])
				// if we either didn't find the first byte OR the first byte of source path was the first
				// byte of the buffer, then we know there is no match here, so copy the whole thing.
				if idx == -1 || idx == 0 {
					// ok, not there at all, easy.
					n := copy(dst, src)
					nSrc = n
					nDst = nSrc
					err = nil
					// fmt.Fprintf(os.Stderr, "no transform copy: %d\n", n)
					return
				} else {
					// ok, there might be something here. Copy the header and
					// return short src
					n := copy(dst, src[:idx])
					nSrc += n
					nDst += nSrc

					fmt.Fprintf(os.Stderr, "possible transform copy: %d (%d)\n", n, len(src))

					src = src[n:]

					err = transform.ErrShortSrc
					return
				}
			} else {
				// ok cool, no more data, copy and be done
				n := copy(dst, src)
				nSrc += n
				nDst += n
				err = nil
				return
			}
		*/
	}

	return
}

// Reset resets the state and allows a Transformer to be reused.
func (n *Archiver) Reset() {
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

	// really large buffer to cope easily with very long lines
	br := bufio.NewReaderSize(nil, 1024*1024)

	for _, file := range files {
		trbuf.Reset()

		f, err := os.Open(file)
		if err != nil {
			return err
		}

		br.Reset(f)

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
