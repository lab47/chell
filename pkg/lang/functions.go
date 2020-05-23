package lang

import (
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
		segments = append(segments, arg.String())
	}

	str := strings.Join(segments, " ")

	fmt.Printf("|> %s\n", str)

	cmd := exec.Command("sh", "-c", str)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env.extraEnv
	cmd.Dir = env.buildDir

	err := cmd.Run()
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

	fmt.Printf("|> append_env: %s => %s", key, value)

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

			fmt.Printf("linking %s from %s\n", target, epath)

			os.MkdirAll(filepath.Dir(target), 0755)

			err = os.Symlink(epath, target)
			if err != nil {
				return starlark.None, err
			}
		}
	case starlark.String:
		target := filepath.Join(target, filepath.Base(string(sv)))
		fmt.Printf("linking %s from %s\n", target, sv)

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
