package ops

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/hashicorp/go-getter"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/cleanhttp"
	"github.com/lab47/chell/pkg/fileutils"
	"github.com/lab47/exprcore/exprcore"
	"github.com/pkg/errors"
	"golang.org/x/crypto/blake2b"
)

type ScriptInstall struct {
	common

	pkg *ScriptPackage
}

func (i *ScriptInstall) setupInputFile(ui *UI, dir string, in ScriptInput) error {
	var (
		tgt, archive string
		dec          getter.Decompressor
	)

	if in.Data.into != "" {
		tgt = filepath.Join(dir, in.Data.into)

		// Allow the into argument to spring parent dirs into existance
		err := os.MkdirAll(filepath.Dir(tgt), 0755)
		if err != nil {
			return err
		}
	} else {
		matchingLen := 0
		for k := range getter.Decompressors {
			if strings.HasSuffix(in.Data.path, "."+k) && len(k) > matchingLen {
				archive = k
				matchingLen = len(k)
			}
		}

		var ok bool

		dec, ok = getter.Decompressors[archive]
		if ok {
			tgt = filepath.Join(dir, in.Name+".data")
		} else {
			tgt = filepath.Join(dir, in.Name+filepath.Ext(in.Data.path))
		}
	}

	f, err := os.Create(tgt)
	if err != nil {
		return err
	}

	defer f.Close()

	if in.Data.data != nil {
		_, err = f.Write(in.Data.data)
		return err
	}

	st, sv, ok := in.Data.Sum()
	if !ok {
		return fmt.Errorf("missing sum: %s", in.Data.path)
	}

	ui.DownloadInput(in.Data.path, st, sv)

	resp, err := cleanhttp.Get(in.Data.path)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	var (
		w io.Writer
		h hash.Hash
	)

	switch st {
	case "b2":
		h, _ = blake2b.New256(nil)
		w = io.MultiWriter(f, h)
	case "sha256":
		h = sha256.New()
		w = io.MultiWriter(f, h)
	case "etag":
		w = f
		// ok
	default:
		return fmt.Errorf("unknown sum type: %s", st)
	}

	io.Copy(w, resp.Body)
	switch st {
	case "etag":
		if string(sv) != resp.Header.Get("Etag") {
			return fmt.Errorf("bad etag sum: %s", in.Data.path)
		}
	default:
		if !bytes.Equal(sv, h.Sum(nil)) {
			return fmt.Errorf("bad sum: %s", in.Data.path)
		}
	}

	// If user specified where to download it to, just leave it as a file.
	if in.Data.into != "" {
		i.L().Trace("setup-input-file: wrote download to path", "path", in.Data.into)
		return nil
	}

	if dec == nil {
		return nil
	}

	i.L().Trace("setup-input-file: unpacking", "path", in.Name)

	target := filepath.Join(dir, in.Name)

	if _, err := os.Stat(target); err == nil {
		return nil
	}

	err = dec.Decompress(target, tgt, true, 0)
	if err != nil {
		return err
	}

	return nil
}

func (i *ScriptInstall) setupInputDir(ui *UI, dir string, in ScriptInput) error {
	name := in.Name

	if name == "" {
		name = filepath.Base(in.Data.dir)
	}

	i.common.L().Trace("setup-input-dir", "name", name, "build-dir", dir, "input-dir", in.Data.dir)

	var inst fileutils.Install
	inst.L = i.common.L()
	inst.Dest = filepath.Join(dir, name)
	inst.Pattern = in.Data.dir

	return inst.Install()
}

func (i *ScriptInstall) setupInputs(ui *UI, dir string) error {
	for _, in := range i.pkg.cs.Inputs {
		if in.Data.dir != "" {
			err := i.setupInputDir(ui, dir, in)
			if err != nil {
				return err
			}
		} else {
			err := i.setupInputFile(ui, dir, in)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *ScriptInstall) Install(ctx context.Context, ienv *InstallEnv) error {
	var thread exprcore.Thread

	log := i.L()
	ui := GetUI(ctx)

	ui.RunScript(i.pkg)

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

	err = i.setupInputs(ui, buildDir)
	if err != nil {
		return track(err)
	}

	var primary *ScriptInput

	if len(i.pkg.cs.Inputs) == 1 {
		primary = &i.pkg.cs.Inputs[0]
	} else {
		for _, i := range i.pkg.cs.Inputs {
			if i.Data.chdir {
				primary = &i
				break
			}
		}
	}

	runDir := buildDir

	if primary != nil {
		checkDir := filepath.Join(runDir, primary.Name)

		sf, err := ioutil.ReadDir(checkDir)
		if err == nil {
			runDir = checkDir
			if len(sf) == 1 && sf[0].IsDir() {
				runDir = filepath.Join(runDir, sf[0].Name())
			}
		}
	}

	var rc RunCtx
	rc.ctx = ctx
	rc.L = log
	rc.attrs = RunCtxFunctions
	rc.installDir = targetDir
	rc.buildDir = runDir
	rc.topDir = buildDir
	rc.outputPrefix = i.pkg.Name()

	args := exprcore.Tuple{&rc}

	var (
		path      []string
		cflags    []string
		ldflags   []string
		pkgconfig []string
	)

	var scd ScriptCalcDeps
	scd.storeDir = ienv.StoreDir

	buildDeps, err := scd.BuildDeps(i.pkg)
	if err != nil {
		return err
	}

	for _, dep := range buildDeps {
		path = append(path, filepath.Join(ienv.StoreDir, dep.ID(), "bin"))

		incpath := filepath.Join(ienv.StoreDir, dep.ID(), "include")
		if _, err := os.Stat(incpath); err == nil {
			cflags = append(cflags, "-I"+incpath)
		}

		libpath := filepath.Join(ienv.StoreDir, dep.ID(), "lib")
		if _, err := os.Stat(libpath); err == nil {
			ldflags = append(ldflags, "-L"+libpath)

			pcpath := filepath.Join(ienv.StoreDir, dep.ID(), "lib", "pkgconfig")
			if _, err := os.Stat(pcpath); err == nil {
				pkgconfig = append(pkgconfig, pcpath)
			}
		}
	}

	path = append(path, "/bin", "/usr/bin")

	rc.path = strings.Join(path, ":")
	rc.extraEnv = []string{"HOME=/nonexistant", "PATH=" + rc.path}

	if len(cflags) > 0 {
		rc.extraEnv = append(rc.extraEnv, "CFLAGS="+strings.Join(cflags, " "))
	}

	if len(ldflags) > 0 {
		rc.extraEnv = append(rc.extraEnv, "LDFLAGS="+strings.Join(ldflags, " "))
	}

	if len(pkgconfig) > 0 {
		rc.extraEnv = append(rc.extraEnv, "PKG_CONFIG_PATH="+strings.Join(pkgconfig, ":"))
	}

	ui.ListDepedencies(buildDeps)

	for _, dep := range buildDeps {
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

	if ienv.StartShell {
		shell := "/bin/bash"

		cmd := exec.Command(shell)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		cmd.Env = append(cmd.Env, rc.extraEnv...)
		cmd.Env = append(cmd.Env, "PREFIX="+targetDir)

		cmd.Dir = runDir

		err = cmd.Run()
		if err != nil {
			return err
		}
	}

	rc.installDir = targetDir
	_, err = exprcore.Call(&thread, i.pkg.cs.Install, args, nil)

	if err != nil {
		log.Error("error running script install", "error", err)
	} else {
		var pan PackageAdjustNames

		perr := pan.Adjust(targetDir)
		if perr != nil {
			log.Error("Error adjusting library names", "error", perr)
		}

		var pwi PackageWriteInfo
		pwi.storeDir = ienv.StoreDir

		_, perr = pwi.Write(i.pkg)
		if perr != nil {
			log.Error("error writing package info", "error", perr)
		}

		var sf StoreFreeze
		sf.storeDir = ienv.StoreDir

		perr = sf.Freeze(i.pkg.ID())
		if perr != nil {
			log.Error("error freezing store dir", "error", perr)
		}
	}

	if ienv.StartShell {
		shell := "/bin/bash"

		cmd := exec.Command(shell)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		cmd.Env = append(cmd.Env, rc.extraEnv...)
		cmd.Env = append(cmd.Env, "PREFIX="+targetDir)

		cmd.Dir = runDir

		cmd.Run()
	}

	return err
}

type RunCtx struct {
	L hclog.Logger

	ctx context.Context

	installDir, buildDir, topDir string
	extraEnv                     []string

	// Used by system, so cached outside extraEnv
	path string

	h io.Writer

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
	case "top":
		return exprcore.String(r.topDir), nil
	}

	val, err := r.attrs.Attr(name)
	if err != nil {
		return nil, err
	}

	return val.(*exprcore.Builtin).BindReceiver(r), nil
}

func (r *RunCtx) AttrNames() []string {
	return append([]string{"prefix", "build", "top"}, r.attrs.AttrNames()...)
}

func noRunRC(v interface{}) (exprcore.Value, error) {
	return nil, fmt.Errorf("no run context bound available: %T", v)
}

var RunCtxFunctions = exprcore.StringDict{
	"system":        exprcore.NewBuiltin("system", systemFn),
	"shell":         exprcore.NewBuiltin("shell", shellFn),
	"apply_patch":   exprcore.NewBuiltin("apply_patch", patchFn),
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
	"mkdir":         exprcore.NewBuiltin("mkdir", mkdirFn),
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

	if err := exprcore.UnpackArgs(
		"chdir", args, kwargs,
		"dir", &dir,
		"fn", &fn,
	); err != nil {
		return nil, err
	}

	if env.h != nil {
		fmt.Fprintf(env.h, "chdir %s\n", dir)
		// Because chdir is pure, run it so we can capture the effects
		// of the lambda
	}

	old := env.buildDir

	defer func() {
		env.buildDir = old
	}()

	env.buildDir = filepath.Join(env.buildDir, dir)

	return exprcore.Call(thread, fn, exprcore.Tuple{}, nil)
}

func mkdirFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	var (
		dir string
	)

	if err := exprcore.UnpackArgs(
		"mkdir", args, kwargs,
		"dir", &dir,
	); err != nil {
		return nil, err
	}

	dir = filepath.Join(env.buildDir, dir)

	if env.h != nil {
		fmt.Fprintf(env.h, "mkdir %s\n", dir)
		return exprcore.None, nil
	}

	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, err
	}

	return exprcore.None, nil
}

func findExecutable(file string) error {
	d, err := os.Stat(file)
	if err != nil {
		return err
	}
	if m := d.Mode(); !m.IsDir() && m&0111 != 0 {
		return nil
	}
	return os.ErrPermission
}

// LookPath searches for an executable named file in the
// directories named by the PATH environment variable.
// If file contains a slash, it is tried directly and the PATH is not consulted.
// The result may be an absolute path or a path relative to the current directory.
func lookPath(file string, path string) (string, error) {
	// NOTE(rsc): I wish we could use the Plan 9 behavior here
	// (only bypass the path if file begins with / or ./ or ../)
	// but that would not match all the Unix shells.

	if strings.Contains(file, "/") {
		err := findExecutable(file)
		if err == nil {
			return file, nil
		}
		return "", err
	}

	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			// Unix shell semantics: path element "" means "."
			dir = "."
		}
		path := filepath.Join(dir, file)
		if err := findExecutable(path); err == nil {
			return path, nil
		}
	}
	return "", errors.Wrapf(ErrNotFound, "unable to find executable: %s", path)
}

func shellFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	var code string

	if err := exprcore.UnpackArgs(
		"shell", args, kwargs,
		"code", &code,
	); err != nil {
		return nil, err
	}

	if env.h != nil {
		fmt.Fprintf(env.h, "shell %s\n", code)
		return exprcore.None, nil
	}

	cmd := exec.Command("bash")
	cmd.Stdin = strings.NewReader(code)
	cmd.Env = env.extraEnv
	cmd.Dir = env.buildDir

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

func patchFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	env, ok := b.Receiver().(*RunCtx)
	if !ok {
		return noRunRC(b.Receiver())
	}

	var patch string

	if err := exprcore.UnpackArgs(
		"patch", args, kwargs,
		"patch", &patch,
	); err != nil {
		return nil, err
	}

	if env.h != nil {
		fmt.Fprintf(env.h, "patch %s\n", args)
		return exprcore.None, nil
	}

	cmd := exec.Command("patch", "-p1")
	cmd.Stdin = strings.NewReader(patch)
	cmd.Env = env.extraEnv
	cmd.Dir = env.buildDir

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

	if dir == "" {
		dir = env.buildDir
	} else {
		dir = filepath.Join(env.buildDir, dir)
	}

	if env.h != nil {
		fmt.Fprintf(env.h, "system %s %s", dir, strings.Join(segments, "::"))
		return exprcore.None, nil
	}

	var err error

	exe := segments[0]

	if filepath.Base(exe) == exe {
		exe, err = lookPath(segments[0], env.path)
		if err != nil {
			return nil, err
		}
	}

	cmd := exec.CommandContext(env.ctx, exe, segments[1:]...)
	cmd.Env = env.extraEnv
	cmd.Dir = dir

	env.L.Debug("invoking system", "command", cmd.Path, "args", cmd.Args, "dir", cmd.Dir)

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

	if env.h != nil {
		fmt.Fprintf(env.h, "inreplace %s %s %s", path, pattern, target)
		return exprcore.None, nil
	}

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

	if env.h != nil {
		fmt.Fprintf(env.h, "inreplace-re %s %s %s", path, pattern, target)
		return exprcore.None, nil
	}

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
		"rmrf", args, kwargs,
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

	target := filepath.Join(env.buildDir, path)

	if env.h != nil {
		fmt.Fprintf(env.h, "rmrf %s %s %s", path, target)
		return exprcore.None, nil
	}

	err = os.RemoveAll(target)
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

	if env.h != nil {
		fmt.Fprintf(env.h, "set-env %s %s", key, value)
		return exprcore.None, nil
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

	if env.h != nil {
		fmt.Fprintf(env.h, "append-env %s %s", key, value)
		return exprcore.None, nil
	}

	if key == "PATH" {
		env.path = env.path + ":" + value
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

	if env.h != nil {
		fmt.Fprintf(env.h, "prepend-env %s %s", key, value)
		return exprcore.None, nil
	}

	if key == "PATH" {
		env.path = value + ":" + env.path
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

			if env.h != nil {
				fmt.Fprintf(env.h, "symlink %s %s", epath, target)
				continue
			}

			env.L.Debug("symlinking", "old-path", epath, "new-path", target)

			os.MkdirAll(filepath.Dir(target), 0755)

			err = os.Symlink(epath, target)
			if err != nil {
				return exprcore.None, err
			}
		}
	case exprcore.String:
		target := filepath.Join(target, filepath.Base(string(sv)))
		if env.h != nil {
			fmt.Fprintf(env.h, "symlink %s %s", string(sv), target)
			break
		}

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

	if env.h != nil {
		fmt.Fprintf(env.h, "install %s %s\n", pattern, target)
		return exprcore.None, nil
	}

	var inst fileutils.Install
	inst.L = env.L
	inst.Dest = target
	inst.Pattern = pattern
	inst.Linked = symlink

	return exprcore.None, inst.Install()
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

	if env.h != nil {
		fmt.Fprintf(env.h, "write-file %s %s\n", target, data)
		return exprcore.None, nil
	}

	f, err := os.Create(target)
	if err != nil {
		return exprcore.None, err
	}

	defer f.Close()

	_, err = f.WriteString(data)

	return exprcore.None, err
}
