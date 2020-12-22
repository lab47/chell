package ops

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-getter"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/cleanhttp"
	"github.com/lab47/chell/pkg/fileutils"
	"github.com/lab47/exprcore/exprcore"
	"golang.org/x/crypto/blake2b"
)

type ScriptInstall struct {
	pkg *ScriptPackage
}

func (i *ScriptInstall) setupInputs(dir string) error {
	for _, in := range i.pkg.cs.Inputs {
		fpath := filepath.Join(dir, in.Name+".data")
		f, err := os.Create(fpath)
		if err != nil {
			return err
		}

		defer f.Close()

		resp, err := cleanhttp.Get(in.Data.path)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		spew.Dump(resp.Header)
		fmt.Printf("=> %s = %s\n", in.Name, in.Data.path)

		h, _ := blake2b.New256(nil)

		io.Copy(io.MultiWriter(f, h), resp.Body)

		st, sv, ok := in.Data.Sum()
		if !ok {
			return fmt.Errorf("missing sum: %s", in.Data.path)
		}

		switch st {
		case "etag":
			if string(sv) != resp.Header.Get("Etag") {
				spew.Dump(sv)
				spew.Dump(resp.Header.Get("Etag"))
				return fmt.Errorf("bad etag sum: %s", in.Data.path)
			}
		case "b2":
			if !bytes.Equal(sv, h.Sum(nil)) {
				return fmt.Errorf("bad sum: %s", in.Data.path)
			}
		default:
			return fmt.Errorf("unknown sum type: %s", st)
		}

		archive := ""
		matchingLen := 0
		for k := range getter.Decompressors {
			if strings.HasSuffix(in.Data.path, "."+k) && len(k) > matchingLen {
				archive = k
				matchingLen = len(k)
			}
		}

		target := filepath.Join(dir, in.Name)

		if _, err := os.Stat(target); err == nil {
			return nil
		}

		dec, ok := getter.Decompressors[archive]
		if !ok {
			return fmt.Errorf("unknown archive type: %s", in.Data.path)
		}

		err = dec.Decompress(target, fpath, true, 0)
		if err != nil {
			return err
		}
	}

	return nil
}

func (i *ScriptInstall) Install(ctx context.Context, ienv *InstallEnv) error {
	var thread exprcore.Thread

	log := hclog.FromContext(ctx)

	buildDir := filepath.Join(ienv.BuildDir, "build-"+i.pkg.ID())
	targetDir := filepath.Join(ienv.StoreDir, i.pkg.ID())

	err := os.Mkdir(buildDir, 0755)
	if err != nil {
		return err
	}

	defer os.RemoveAll(buildDir)

	err = os.Mkdir(targetDir, 0755)
	if err != nil {
		return err
	}

	err = i.setupInputs(buildDir)
	if err != nil {
		return err
	}

	runDir := buildDir

	if len(i.pkg.cs.Inputs) == 1 {
		runDir = filepath.Join(runDir, i.pkg.cs.Inputs[0].Name)

		sf, err := ioutil.ReadDir(runDir)
		if err != nil {
			return err
		}

		if len(sf) == 1 && sf[0].IsDir() {
			runDir = filepath.Join(runDir, sf[0].Name())
		}
	}

	var rc RunCtx
	rc.L = log
	rc.attrs = RunCtxFunctions
	rc.installDir = targetDir
	rc.buildDir = runDir

	args := exprcore.Tuple{&rc}

	var path []string

	for _, dep := range i.pkg.Dependencies() {
		path = append(path, filepath.Join(ienv.StoreDir, dep.ID(), "bin"))
	}

	path = append(path, "/bin", "/usr/bin")

	rc.extraEnv = []string{"HOME=/nonexistant", "PATH=" + strings.Join(path, ":")}

	for _, dep := range i.pkg.Dependencies() {
		hook := dep.cs.Hook
		if hook == nil {
			continue
		}

		rc.installDir = filepath.Join(ienv.StoreDir, dep.ID())

		_, err := exprcore.Call(&thread, hook, args, nil)
		if err != nil {
			return err
		}
	}

	rc.installDir = targetDir
	_, err = exprcore.Call(&thread, i.pkg.cs.Install, args, nil)
	return err
}

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
	"install_files": exprcore.NewBuiltin("install_files", installFn),
	"write_file":    exprcore.NewBuiltin("write_file", writeFileFn),
	"chdir":         exprcore.NewBuiltin("chdir", chdirFn),
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
				return exprcore.None, fmt.Errorf("expected a string to system")
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
