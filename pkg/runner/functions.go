package runner

import (
	"archive/tar"
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/fileutils"
	"github.com/lab47/exprcore/exprcore"
	"github.com/mholt/archiver/v3"
)

var ErrExpectedString = errors.New("expected string value")

type RunCtx struct {
	L hclog.Logger

	installDir, buildDir string
	extraEnv             []string

	outputPrefix string

	attrs exprcore.StringDict
}

// String returns the string representation of the value.
// exprcore string values are quoted as if by Python's repr.
func (r *RunCtx) String() string {
	return "<runctx>"
}

// Type returns a short string describing the value's type.
func (r *RunCtx) Type() string {
	return "<runctx>"
}

// Freeze causes the value, and all values transitively
// reachable from it through collections and closures, to be
// marked as frozen.  All subsequent mutations to the data
// structure through this API will fail dynamically, making the
// data structure immutable and safe for publishing to other
// exprcore interpreters running concurrently.
func (r *RunCtx) Freeze() {
}

// Truth returns the truth value of an object.
func (r *RunCtx) Truth() exprcore.Bool {
	return exprcore.True
}

// Hash returns a function of x such that Equals(x, y) => Hash(x) == Hash(y).
// Hash may fail if the value's type is not hashable, or if the value
// contains a non-hashable value. The hash is used only by dictionaries and
// is not exposed to the exprcore program.
func (r *RunCtx) Hash() (uint32, error) {
	return 0, fmt.Errorf("not hashable")
}

func (r *RunCtx) Attr(name string) (exprcore.Value, error) {
	switch name {
	case "prefix":
		return exprcore.String(r.installDir), nil
	case "build":
		return exprcore.String(r.buildDir), nil
	}

	val, err := r.attrs.Attr(name)
	if err != nil {
		return nil, err
	}

	return val.(*exprcore.Builtin).BindReceiver(r), nil
}

func (r *RunCtx) AttrNames() []string {
	return append([]string{"prefix", "build"}, r.attrs.AttrNames()...)
}

func noRunRC(v interface{}) (exprcore.Value, error) {
	return nil, fmt.Errorf("no run context bound available: %T", v)
}

var RunCtxFunctions = exprcore.StringDict{
	"system":        exprcore.NewBuiltin("system", systemFn),
	"inreplace":     exprcore.NewBuiltin("inreplace", inreplaceFn),
	"inreplace_re":  exprcore.NewBuiltin("inreplace_re", inreplaceReFn),
	"rm_f":          exprcore.NewBuiltin("rm_f", rmrfFn),
	"rm_rf":         exprcore.NewBuiltin("rm_rf", rmrfFn),
	"set_env":       exprcore.NewBuiltin("set_env", setEnvFn),
	"append_env":    exprcore.NewBuiltin("append_env", appendEnvFn),
	"prepend_env":   exprcore.NewBuiltin("prepend_env", prependEnvFn),
	"link":          exprcore.NewBuiltin("link", linkFn),
	"unpack":        exprcore.NewBuiltin("unpack", unpackFn),
	"install_files": exprcore.NewBuiltin("install_files", installFn),
	"write_file":    exprcore.NewBuiltin("write_file", writeFileFn),
	"chdir":         exprcore.NewBuiltin("chdir", chdirFn),
	"auto_chdir":    exprcore.NewBuiltin("auto_chdir", autoChdirFn),
}

func chdirFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	var (
		dir string
		fn  exprcore.Callable
	)

	exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"dir", &dir,
		"fn", &fn,
	)

	old := env.buildDir

	defer func() {
		env.buildDir = old
	}()

	env.buildDir = filepath.Join(env.buildDir, dir)

	return exprcore.Call(thread, fn, exprcore.Tuple{}, nil)
}

func autoChdirFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	if err := exprcore.UnpackArgs(
		"auto_chdir", args, kwargs,
	); err != nil {
		return nil, err
	}

	files, err := ioutil.ReadDir(env.buildDir)
	if err != nil {
		return nil, err
	}

	if len(files) > 1 {
		return nil, fmt.Errorf("multiple inputs, no auto chdir available")
	}

	fi := files[0]

	if !fi.IsDir() {
		return nil, fmt.Errorf("toplevel entry is not a directory")
	}

	dir := filepath.Join(env.buildDir, fi.Name())

	for {
		sf, err := ioutil.ReadDir(dir)
		if err != nil {
			return nil, err
		}

		if len(sf) > 1 {
			break
		}

		if !sf[0].IsDir() {
			break
		}

		dir = filepath.Join(dir, sf[0].Name())
	}

	env.buildDir = dir

	return exprcore.String(dir), nil
}

func systemFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	var segments []string

	for _, arg := range args {
		switch sv := arg.(type) {
		case exprcore.String:
			segments = append(segments, string(sv))
		default:
			segments = append(segments, arg.String())
		}
	}

	var dir string

	for _, item := range kwargs {
		name, arg := item[0].(exprcore.String), item[1]
		if name == "dir" {
			s, ok := arg.(exprcore.String)
			if !ok {
				return exprcore.None, ErrExpectedString
			}

			dir = string(s)
		}
	}

	env.L.Debug("invoking system", "command", segments)

	cmd := exec.Command(segments[0], segments[1:]...)

	cmd.Env = env.extraEnv
	if dir == "" {
		cmd.Dir = env.buildDir
	} else {
		cmd.Dir = filepath.Join(env.buildDir, dir)
	}

	spew.Dump(env.extraEnv)

	or, err := cmd.StdoutPipe()
	if err != nil {
		return exprcore.None, err
	}

	er, err := cmd.StderrPipe()
	if err != nil {
		return exprcore.None, err
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		br := bufio.NewReader(or)
		for {
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				fmt.Printf("%s │ %s\n", env.outputPrefix, strings.TrimRight(line, " \n\t"))
			}

			if err != nil {
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		br := bufio.NewReader(er)
		for {
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				fmt.Printf("%s │ %s\n", env.outputPrefix, strings.TrimRight(line, " \n\t"))
			}

			if err != nil {
				return
			}
		}
	}()

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	wg.Wait()

	err = cmd.Wait()
	if err != nil {
		return nil, err
	}

	return exprcore.None, nil
}

func basicFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var name string

	exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"name", &name,
	)

	return exprcore.None, nil
}

func checkPath(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path, contains ..")
	}

	return nil
}

func inreplaceFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var file, pattern, target string

	err := exprcore.UnpackArgs(
		"inreplace", args, kwargs,
		"file", &file,
		"pattern", &pattern,
		"target", &target,
	)

	if err != nil {
		return exprcore.None, err
	}

	err = checkPath(file)
	if err != nil {
		return exprcore.None, err
	}

	rep := strings.NewReplacer(pattern, target)

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	path := filepath.Join(env.buildDir, file)

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return exprcore.None, err
	}

	f, err := os.Create(path)
	if err != nil {
		return exprcore.None, err
	}

	defer f.Close()

	_, err = rep.WriteString(f, string(data))
	if err != nil {
		return exprcore.None, err
	}

	return exprcore.None, nil
}

func inreplaceReFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var file, pattern, target string

	err := exprcore.UnpackArgs(
		"inreplace", args, kwargs,
		"file", &file,
		"pattern", &pattern,
		"target", &target,
	)

	if err != nil {
		return exprcore.None, err
	}

	err = checkPath(file)
	if err != nil {
		return exprcore.None, err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return exprcore.None, err
	}

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	path := filepath.Join(env.buildDir, file)

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return exprcore.None, err
	}

	f, err := os.Create(path)
	if err != nil {
		return exprcore.None, err
	}

	defer f.Close()

	data = re.ReplaceAll(data, []byte(target))

	_, err = f.Write(data)
	if err != nil {
		return exprcore.None, err
	}

	return exprcore.None, nil
}

func rmrfFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var path string

	err := exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"path", &path,
	)

	if err != nil {
		return exprcore.None, err
	}

	err = checkPath(path)
	if err != nil {
		return exprcore.None, err
	}

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	err = os.RemoveAll(filepath.Join(env.buildDir, path))
	if err != nil {
		return exprcore.None, err
	}

	return exprcore.None, nil
}

func setEnvFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var key, value string

	err := exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"key", &key,
		"value", &value,
	)

	if err != nil {
		return exprcore.None, err
	}

	fmt.Printf("in set-env...\n")

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	env.extraEnv = append(env.extraEnv, key+"="+value)

	return exprcore.None, nil
}

func appendEnvFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var key, value string

	err := exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"key", &key,
		"value", &value,
	)

	if err != nil {
		return exprcore.None, err
	}

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	prefix := key + "="

	for i, kv := range env.extraEnv {
		if strings.HasPrefix(kv, prefix) {
			env.extraEnv[i] += (string(filepath.ListSeparator) + value)
			return exprcore.None, nil
		}
	}

	env.extraEnv = append(env.extraEnv, key+"="+value)

	return exprcore.None, nil
}

func prependEnvFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var key, value string

	err := exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"key", &key,
		"value", &value,
	)

	if err != nil {
		return exprcore.None, err
	}

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	prefix := key + "="

	for i, kv := range env.extraEnv {
		if strings.HasPrefix(kv, prefix) {
			env.extraEnv[i] = value + string(filepath.ListSeparator) + env.extraEnv[i]
			return exprcore.None, nil
		}
	}

	env.extraEnv = append(env.extraEnv, key+"="+value)

	return exprcore.None, nil
}

func linkFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var path exprcore.Value
	var target string

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	err := exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"path", &path,
		"target", &target,
	)

	if err != nil {
		return exprcore.None, err
	}

	switch sv := path.(type) {
	case *exprcore.List:
		iter := sv.Iterate()
		defer iter.Done()

		var ele exprcore.Value
		for iter.Next(&ele) {
			var epath string

			if str, ok := ele.(exprcore.String); ok {
				epath = string(str)
			} else {
				epath = ele.String()
			}

			target := filepath.Join(target, filepath.Base(epath))

			env.L.Debug("symlinking", "old-path", epath, "new-path", target)

			os.MkdirAll(filepath.Dir(target), 0755)

			err = os.Symlink(epath, target)
			if err != nil {
				return exprcore.None, err
			}
		}
	case exprcore.String:
		target := filepath.Join(target, filepath.Base(string(sv)))
		env.L.Debug("symlinking", "old-path", string(sv), "new-path", target)

		os.MkdirAll(filepath.Dir(target), 0755)

		err = os.Symlink(string(sv), target)
		if err != nil {
			return exprcore.None, err
		}
	}

	return exprcore.None, nil
}

func writeNewFile(fpath string, in io.Reader, fm os.FileMode) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %v", fpath, err)
	}

	out, err := os.Create(fpath)
	if err != nil {
		return fmt.Errorf("%s: creating new file: %v", fpath, err)
	}
	defer out.Close()

	err = out.Chmod(fm)
	if err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("%s: changing file mode: %v", fpath, err)
	}

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("%s: writing file: %v", fpath, err)
	}
	return nil
}

func writeNewSymbolicLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %v", fpath, err)
	}

	_, err = os.Lstat(fpath)
	if err == nil {
		err = os.Remove(fpath)
		if err != nil {
			return fmt.Errorf("%s: failed to unlink: %+v", fpath, err)
		}
	}

	err = os.Symlink(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making symbolic link for: %v", fpath, err)
	}
	return nil
}

func writeNewHardLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %v", fpath, err)
	}

	_, err = os.Lstat(fpath)
	if err == nil {
		err = os.Remove(fpath)
		if err != nil {
			return fmt.Errorf("%s: failed to unlink: %+v", fpath, err)
		}
	}

	err = os.Link(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making hard link for: %v", fpath, err)
	}
	return nil
}

func unpackFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var path, sha256, target string

	err := exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"path", &path,
		"sha256", &sha256,
		"target", &target,
	)

	if err != nil {
		return exprcore.None, err
	}

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	spath := filepath.Join(env.buildDir, filepath.Base(path))

	env.L.Debug("downloading for unpack", "url", path, "target", spath)

	resp, err := http.Get(path)
	if err != nil {
		return exprcore.None, err
	}

	defer resp.Body.Close()

	f, err := os.Create(spath)
	if err != nil {
		return exprcore.None, err
	}

	io.Copy(f, resp.Body)

	ar, err := archiver.ByExtension(spath)
	if err != nil {
		return exprcore.None, err
	}

	ua, ok := ar.(archiver.Walker)
	if !ok {
		return exprcore.None, fmt.Errorf("unknown source compression format")
	}

	target = filepath.Join(env.buildDir, target)

	if _, err := os.Stat(target); err != nil {
		err = os.MkdirAll(target, 0755)
		if err != nil {
			return exprcore.None, err
		}
	}

	err = ua.Walk(spath, func(f archiver.File) error {
		hdr, ok := f.Header.(*tar.Header)
		if !ok {
			return fmt.Errorf("expected header to be *tar.Header but was %T", f.Header)
		}

		name := hdr.Name

		// strip the initial directory
		idx := strings.IndexByte(name, '/')
		if idx != -1 {
			name = name[idx+1:]
			if name == "" {
				// toplevel, skip
				return nil
			}
		} else if f.IsDir() {
			// toplevel dir, skip
			return nil
		}

		to := filepath.Join(target, name)

		// do not overwrite existing files, if configured
		if _, err := os.Stat(to); err == nil && !f.IsDir() {
			return fmt.Errorf("file already exists: %s", to)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			return os.Mkdir(to, f.Mode())
		case tar.TypeReg, tar.TypeRegA, tar.TypeChar, tar.TypeBlock, tar.TypeFifo, tar.TypeGNUSparse:
			return writeNewFile(to, f, f.Mode())
		case tar.TypeSymlink:
			return writeNewSymbolicLink(to, hdr.Linkname)
		case tar.TypeLink:
			return writeNewHardLink(to, filepath.Join(to, hdr.Linkname))
		case tar.TypeXGlobalHeader:
			return nil // ignore the pax global header from git-generated tarballs
		default:
			return fmt.Errorf("%s: unknown type flag: %c", hdr.Name, hdr.Typeflag)
		}
	})

	if err != nil {
		return exprcore.None, err
	}

	return exprcore.None, nil
}

func installFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var (
		target, pattern string
		symlink         bool
	)

	err := exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"target", &target,
		"pattern", &pattern,
		"symlink?", &symlink,
	)

	if err != nil {
		return exprcore.None, err
	}

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(env.buildDir, pattern)
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(env.installDir, target)
	}

	var inst fileutils.Install
	inst.Dest = target
	inst.Pattern = pattern
	inst.Linked = symlink

	return exprcore.None, inst.Install()
}

func globFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var (
		pattern string
		fn      exprcore.Callable
	)

	err := exprcore.UnpackArgs(
		"glob", args, kwargs,
		"pattern", &pattern,
		"fn", &fn,
	)

	if err != nil {
		return exprcore.None, err
	}

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	dir := filepath.Join(env.buildDir, filepath.Dir(pattern))

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if matched, _ := filepath.Match(pattern, path); matched {
			_, err = exprcore.Call(thread, fn, exprcore.Tuple{exprcore.String(path)}, nil)
			return err
		}

		return nil
	})

	return exprcore.None, nil
}

func writeFileFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var (
		target, data string
	)

	err := exprcore.UnpackArgs(
		"write_file", args, kwargs,
		"target", &target,
		"data", &data,
	)

	if err != nil {
		return exprcore.None, err
	}

	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(env.installDir, target)
	}

	f, err := os.Create(target)
	if err != nil {
		return exprcore.None, err
	}

	defer f.Close()

	_, err = f.WriteString(data)

	return exprcore.None, err
}
