package lang

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

	"github.com/evanphx/chell/pkg/chell"
	"github.com/evanphx/chell/pkg/fileutils"
	"github.com/evanphx/chell/pkg/resolver"
	"github.com/lab47/exprcore/exprcore"
	"github.com/mholt/archiver/v3"
)

func pkgFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var (
		name, version, source, sha256 string
		deps                          *exprcore.List
		install                       *exprcore.Function
	)

	exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"name", &name,
		"version", &version,
		"source", &source,
		"sha256", &sha256,
		"dependencies", &deps,
		"install", &install,
	)

	pkg := &PackageValue{
		Package: chell.Package{
			Name:    name,
			Version: version,
			Source:  source,
			Sha256:  sha256,
		},
		install: install,
	}

	if deps != nil {
		iter := deps.Iterate()
		defer iter.Done()
		var x exprcore.Value
		for iter.Next(&x) {
			if str, ok := x.(exprcore.String); ok {
				pkg.Deps.Runtime = append(pkg.Deps.Runtime, string(str))
			}
		}
	}

	thread.SetLocal("pkg", pkg)

	return pkg, nil
}

var ErrExpectedString = errors.New("expected string value")

func systemFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	env := thread.Local("install-env").(*installEnv)

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

	if env.h != nil {
		for _, seg := range segments {
			fmt.Fprintln(env.h, seg)
		}
		fmt.Fprintln(env.h, strings.Join(env.extraEnv, ":"))

		if env.hashOnly {
			return exprcore.None, nil
		}
	}

	cmd := exec.Command(segments[0], segments[1:]...)
	or, err := cmd.StdoutPipe()
	if err != nil {
		return exprcore.None, err
	}
	er, err := cmd.StderrPipe()
	if err != nil {
		return exprcore.None, err
	}

	cmd.Env = env.extraEnv
	if dir == "" {
		cmd.Dir = env.buildDir
	} else {
		cmd.Dir = filepath.Join(env.buildDir, dir)
	}

	go func() {
		br := bufio.NewReader(or)
		for {
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				fmt.Printf("%s │ %s", env.outputPrefix, line)
			}

			if err != nil {
				return
			}
		}
	}()

	go func() {
		br := bufio.NewReader(er)
		for {
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				fmt.Printf("%s │ %s", env.outputPrefix, line)
			}

			if err != nil {
				return
			}
		}
	}()

	err = cmd.Run()
	if err != nil {
		return nil, err
	}

	return exprcore.None, nil
}

func resolveFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var res *resolver.Resolver

	val := thread.Local("resolver")
	if val == nil {
		env := thread.Local("install-env").(*installEnv)

		var re resolver.Resolver
		re.StorePath = env.storeDir

		res = &re

		thread.SetLocal("resolver", res)
	} else {
		res = val.(*resolver.Resolver)
	}

	var name string

	exprcore.UnpackArgs(
		"pkg", args, kwargs,
		"name", &name,
	)

	path, err := res.Resolve(name)
	if err != nil {
		return nil, err
	}

	if path == "" {
		return nil, fmt.Errorf("unknown dependency: %s", name)
	}

	return exprcore.String(path), nil
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

	env := thread.Local("install-env").(*installEnv)

	if env.h != nil {
		fmt.Fprintf(env.h, "inreplace `%s` `%s` `%s`\n", file, pattern, target)

		if env.hashOnly {
			return exprcore.None, nil
		}
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

	env := thread.Local("install-env").(*installEnv)

	if env.h != nil {
		fmt.Fprintf(env.h, "inreplace `%s` `%s` `%s`\n", file, pattern, target)

		if env.hashOnly {
			return exprcore.None, nil
		}
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

	env := thread.Local("install-env").(*installEnv)

	if env.h != nil {
		fmt.Fprintf(env.h, "rmrf `%s`\n", path)

		if env.hashOnly {
			return exprcore.None, nil
		}
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

	env := thread.Local("install-env").(*installEnv)

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

	env := thread.Local("install-env").(*installEnv)

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

	env := thread.Local("install-env").(*installEnv)

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

	env := thread.Local("install-env").(*installEnv)

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

			if env.h != nil {
				fmt.Fprintf(env.h, "link `%s` `%s`\n", epath, target)

				if env.hashOnly {
					continue
				}
			}

			os.MkdirAll(filepath.Dir(target), 0755)

			err = os.Symlink(epath, target)
			if err != nil {
				return exprcore.None, err
			}
		}
	case exprcore.String:
		target := filepath.Join(target, filepath.Base(string(sv)))
		env.L.Debug("symlinking", "old-path", string(sv), "new-path", target)

		if env.h != nil {
			fmt.Fprintf(env.h, "link `%s` `%s`\n", string(sv), target)

			if env.hashOnly {
				break
			}
		}

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

	env := thread.Local("install-env").(*installEnv)

	if env.h != nil {
		fmt.Fprintf(env.h, "unpack:%s/%s", path, sha256)

		if env.hashOnly {
			return exprcore.None, nil
		}
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

	env := thread.Local("install-env").(*installEnv)

	if env.h != nil {
		fmt.Fprintf(env.h, "install:%s/%s/%v", target, pattern, symlink)

		if env.hashOnly {
			return exprcore.None, nil
		}
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

func makeFuncs() exprcore.StringDict {
	return exprcore.StringDict{
		"system":        exprcore.NewBuiltin("system", systemFn),
		"pkg":           exprcore.NewBuiltin("pkg", pkgFn),
		"resolve":       exprcore.NewBuiltin("resolve", resolveFn),
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
	}
}
