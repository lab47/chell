package lang

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/evanphx/chell/pkg/chell"
	"github.com/evanphx/chell/pkg/resolver"
	"go.starlark.net/starlark"
)

func pkgFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name, version, source, sha256 string
	var deps *starlark.List

	starlark.UnpackArgs(
		"pkg", args, kwargs,
		"name", &name,
		"version", &version,
		"source", &source,
		"sha256", &sha256,
		"dependencies", &deps,
	)

	pkg := &PackageValue{
		Package: chell.Package{
			Name:    name,
			Version: version,
			Source:  source,
			Sha256:  sha256,
		},
	}

	if deps != nil {
		iter := deps.Iterate()
		defer iter.Done()
		var x starlark.Value
		for iter.Next(&x) {
			if str, ok := x.(starlark.String); ok {
				pkg.Deps.Runtime = append(pkg.Deps.Runtime, string(str))
			}
		}
	}

	thread.SetLocal("pkg", pkg)

	return pkg, nil
}

func systemFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	env := thread.Local("install-env").(*installEnv)

	var segments []string

	for _, arg := range args {
		switch sv := arg.(type) {
		case starlark.String:
			segments = append(segments, string(sv))
		default:
			segments = append(segments, arg.String())
		}
	}

	env.L.Debug("invoking system", "command", segments)

	if env.h != nil {
		for _, seg := range segments {
			fmt.Fprintln(env.h, seg)
		}
		fmt.Fprintln(env.h, strings.Join(env.extraEnv, ":"))

		if env.hashOnly {
			return starlark.None, nil
		}
	}

	cmd := exec.Command(segments[0], segments[1:]...)
	or, err := cmd.StdoutPipe()
	if err != nil {
		return starlark.None, err
	}
	er, err := cmd.StderrPipe()
	if err != nil {
		return starlark.None, err
	}

	cmd.Env = env.extraEnv
	cmd.Dir = env.buildDir

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

	return starlark.None, nil
}

func resolveFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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

	starlark.UnpackArgs(
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

	return starlark.String(path), nil
}

func basicFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string

	starlark.UnpackArgs(
		"pkg", args, kwargs,
		"name", &name,
	)

	return starlark.None, nil
}

func checkPath(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path, contains ..")
	}

	return nil
}

func inreplaceFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var file, pattern, target string

	err := starlark.UnpackArgs(
		"inreplace", args, kwargs,
		"file", &file,
		"pattern", &pattern,
		"target", &target,
	)

	if err != nil {
		return starlark.None, err
	}

	err = checkPath(file)
	if err != nil {
		return starlark.None, err
	}

	rep := strings.NewReplacer(pattern, target)

	env := thread.Local("install-env").(*installEnv)

	if env.h != nil {
		fmt.Fprintf(env.h, "inreplace `%s` `%s` `%s`\n", file, pattern, target)

		if env.hashOnly {
			return starlark.None, nil
		}
	}

	path := filepath.Join(env.buildDir, file)

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return starlark.None, err
	}

	f, err := os.Create(path)
	if err != nil {
		return starlark.None, err
	}

	defer f.Close()

	_, err = rep.WriteString(f, string(data))
	if err != nil {
		return starlark.None, err
	}

	return starlark.None, nil
}

func inreplaceReFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var file, pattern, target string

	err := starlark.UnpackArgs(
		"inreplace", args, kwargs,
		"file", &file,
		"pattern", &pattern,
		"target", &target,
	)

	if err != nil {
		return starlark.None, err
	}

	err = checkPath(file)
	if err != nil {
		return starlark.None, err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return starlark.None, err
	}

	env := thread.Local("install-env").(*installEnv)

	if env.h != nil {
		fmt.Fprintf(env.h, "inreplace `%s` `%s` `%s`\n", file, pattern, target)

		if env.hashOnly {
			return starlark.None, nil
		}
	}

	path := filepath.Join(env.buildDir, file)

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return starlark.None, err
	}

	f, err := os.Create(path)
	if err != nil {
		return starlark.None, err
	}

	defer f.Close()

	data = re.ReplaceAll(data, []byte(target))

	_, err = f.Write(data)
	if err != nil {
		return starlark.None, err
	}

	return starlark.None, nil
}

func rmrfFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string

	err := starlark.UnpackArgs(
		"pkg", args, kwargs,
		"path", &path,
	)

	if err != nil {
		return starlark.None, err
	}

	err = checkPath(path)
	if err != nil {
		return starlark.None, err
	}

	env := thread.Local("install-env").(*installEnv)

	if env.h != nil {
		fmt.Fprintf(env.h, "rmrf `%s`\n", path)

		if env.hashOnly {
			return starlark.None, nil
		}
	}

	err = os.RemoveAll(filepath.Join(env.buildDir, path))
	if err != nil {
		return starlark.None, err
	}

	return starlark.None, nil
}

func setEnvFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key, value string

	err := starlark.UnpackArgs(
		"pkg", args, kwargs,
		"key", &key,
		"value", &value,
	)

	if err != nil {
		return starlark.None, err
	}

	env := thread.Local("install-env").(*installEnv)

	env.extraEnv = append(env.extraEnv, key+"="+value)

	return starlark.None, nil
}

func appendEnvFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key, value string

	err := starlark.UnpackArgs(
		"pkg", args, kwargs,
		"key", &key,
		"value", &value,
	)

	if err != nil {
		return starlark.None, err
	}

	env := thread.Local("install-env").(*installEnv)

	prefix := key + "="

	for i, kv := range env.extraEnv {
		if strings.HasPrefix(kv, prefix) {
			env.extraEnv[i] += (string(filepath.ListSeparator) + value)
			return starlark.None, nil
		}
	}

	env.extraEnv = append(env.extraEnv, key+"="+value)

	return starlark.None, nil
}

func linkFn(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path starlark.Value
	var target string

	env := thread.Local("install-env").(*installEnv)

	err := starlark.UnpackArgs(
		"pkg", args, kwargs,
		"path", &path,
		"target", &target,
	)

	if err != nil {
		return starlark.None, err
	}

	switch sv := path.(type) {
	case *starlark.List:
		iter := sv.Iterate()
		defer iter.Done()

		var ele starlark.Value
		for iter.Next(&ele) {
			var epath string

			if str, ok := ele.(starlark.String); ok {
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
				return starlark.None, err
			}
		}
	case starlark.String:
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
			return starlark.None, err
		}
	}

	return starlark.None, nil
}

func makeFuncs() starlark.StringDict {
	return starlark.StringDict{
		"system":       starlark.NewBuiltin("system", systemFn),
		"pkg":          starlark.NewBuiltin("pkg", pkgFn),
		"resolve":      starlark.NewBuiltin("resolve", resolveFn),
		"inreplace":    starlark.NewBuiltin("inreplace", inreplaceFn),
		"inreplace_re": starlark.NewBuiltin("inreplace_re", inreplaceReFn),
		"rm_f":         starlark.NewBuiltin("rm_f", rmrfFn),
		"set_env":      starlark.NewBuiltin("set_env", setEnvFn),
		"append_env":   starlark.NewBuiltin("append_env", appendEnvFn),
		"link":         starlark.NewBuiltin("link", linkFn),
	}
}
