package lang

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanphx/chell/pkg/builder"
	"github.com/evanphx/chell/pkg/chell"
	"github.com/evanphx/chell/pkg/resolver"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/hashstructure"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"golang.org/x/crypto/blake2b"
)

type Function struct {
	Code string

	Package *chell.Package
	install *starlark.Function
	hook    *starlark.Function

	installDir string
	buildDir   string

	Dependencies []*Function
}

func Translate(pkg *chell.Package) (*Function, error) {
	var f Function

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "pkg(\n")
	fmt.Fprintf(&buf, "  name = \"%s\",\n", pkg.Name)
	fmt.Fprintf(&buf, "  version = \"%s\",\n", pkg.Version)
	fmt.Fprintf(&buf, "  source = \"%s\",\n", pkg.Source)
	fmt.Fprintf(&buf, "  sha256 = \"%s\",\n", pkg.Sha256)
	if len(pkg.Deps.Runtime) > 0 {
		fmt.Fprintf(&buf, "  dependencies = [\n")
		for _, dep := range pkg.Deps.Runtime {
			fmt.Fprintf(&buf, "    resolve(\"%s\"),\n", dep)
		}
		fmt.Fprintf(&buf, "  ],\n")
	}
	fmt.Fprintf(&buf, ")\n\n")

	fmt.Fprintf(&buf, "def install(prefix, build):\n")

	for _, line := range pkg.Install {
		fmt.Fprintf(&buf, "  %s\n", line)
	}

	f.Code = buf.String()

	return &f, nil
}

type installEnv struct {
	installDir, buildDir, storeDir string
	extraEnv                       []string
}

type PackageValue struct {
	chell.Package

	frozen bool
}

func (p *PackageValue) String() string {
	return "pkg: " + p.Name
}

// Type returns a short string describing the value's type.
func (p *PackageValue) Type() string {
	return "Package"
}

// Freeze causes the value, and all values transitively
// reachable from it through collections and closures, to be
// marked as frozen.  All subsequent mutations to the data
// structure through this API will fail dynamically, making the
// data structure immutable and safe for publishing to other
// Starlark interpreters running concurrently.
func (p *PackageValue) Freeze() {
	p.frozen = true
}

// Truth returns the truth value of an object.
func (p *PackageValue) Truth() starlark.Bool {
	return starlark.True
}

// Hash returns a function of x such that Equals(x, y) => Hash(x) == Hash(y).
// Hash may fail if the value's type is not hashable, or if the value
// contains a non-hashable value. The hash is used only by dictionaries and
// is not exposed to the Starlark program.
func (p *PackageValue) Hash() (uint32, error) {
	h, err := hashstructure.Hash(p, nil)
	if err != nil {
		return 0, err
	}

	return uint32(h), nil
}

func Locate(path, storeDir, pkgPath string) (*Function, error) {
	for _, dir := range filepath.SplitList(pkgPath) {
		tp := filepath.Join(dir, path) + ".chell"

		fmt.Printf("checking %s\n", tp)

		if _, err := os.Stat(tp); err == nil {
			return Load(tp, storeDir, pkgPath)
		}
	}

	return nil, fmt.Errorf("unable to locate package definition: %s", path)
}

func Load(path, storeDir, pkgPath string) (*Function, error) {
	vars := makeFuncs()

	isPD := vars.Has

	_, prog, err := starlark.SourceProgram(path, nil, isPD)
	if err != nil {
		return nil, err
	}

	fn := &Function{}

	var thread starlark.Thread

	thread.SetLocal("install-env", &installEnv{
		storeDir: storeDir,
	})

	glb, err := prog.Init(&thread, vars)
	if err != nil {
		return nil, err
	}

	vpkg := thread.Local("pkg")
	if vpkg == nil {
		return nil, fmt.Errorf("no pkg call made")
	}

	pkg := vpkg.(*PackageValue)

	fn.Package = &pkg.Package

	if f, ok := glb["install"].(*starlark.Function); ok {
		fn.install = f
	}

	if f, ok := glb["hook"].(*starlark.Function); ok {
		fn.hook = f
	}

	for _, dep := range fn.Package.Deps.Runtime {
		sub, err := Locate(dep, storeDir, pkgPath)
		if err != nil {
			return nil, err
		}

		fn.Dependencies = append(fn.Dependencies, sub)
	}

	return fn, nil
}

func (f *Function) Install(ctx context.Context, L hclog.Logger, buildDir, storeDir string) (string, error) {
	buildDir, err := filepath.Abs(buildDir)
	if err != nil {
		return "", err
	}

	storeDir, err = filepath.Abs(storeDir)
	if err != nil {
		return "", err
	}

	var res resolver.Resolver
	res.StorePath = storeDir

	spec := &builder.Spec{
		Name:    f.Package.Name,
		Version: f.Package.Version,
		Source:  f.Package.Source,
	}

	pkgs := starlark.StringDict{}

	for _, dep := range f.Package.Deps.Runtime {
		sp, err := res.Resolve(dep)
		if err != nil {
			return "", err
		}

		spec.Dependencies = append(spec.Dependencies, sp)

		pkgs[dep] = starlark.String(filepath.Join(storeDir, sp))
	}

	os.MkdirAll(buildDir, 0755)

	env := &builder.Env{
		BuildDir: buildDir,
		StoreDir: storeDir,
	}

	h, _ := blake2b.New(16, nil)
	fmt.Fprintln(h, f.Code)

	var (
		pathParts []string
		buildEnv  []string
	)

	for _, pp := range spec.Dependencies {
		pathParts = append(pathParts, filepath.Join(storeDir, pp, "bin"))
		// buildEnv = append(buildEnv, f.Package.Deps.Runtime[i]+"="+filepath.Join(storeDir, pp))
	}

	path := strings.Join(pathParts, string(filepath.ListSeparator)) + ":/bin:/usr/bin"

	fn := func(buildDir, installDir string) ([]byte, error) {
		var thread starlark.Thread

		thread.SetLocal("install-env", &installEnv{
			buildDir:   buildDir,
			installDir: installDir,
			storeDir:   storeDir,
			extraEnv:   append(buildEnv, "HOME=/nonexistant", "PATH="+path),
		})

		thread.SetLocal("resolver", res)

		for i, dep := range f.Dependencies {
			L.Trace("consider hook", "pkg", dep.Package.Name, "hook", dep.hook)
			if dep.hook == nil {
				continue
			}

			ictx := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
				"prefix": starlark.String(filepath.Join(storeDir, spec.Dependencies[i])),
				"build":  starlark.String(buildDir),
			})

			args := starlark.Tuple{ictx}

			_, err := starlark.Call(&thread, dep.hook, args, nil)
			if err != nil {
				return nil, err
			}
		}

		ictx := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
			"prefix":  starlark.String(installDir),
			"build":   starlark.String(buildDir),
			"pkgs":    starlarkstruct.FromStringDict(starlarkstruct.Default, pkgs),
			"head_eh": starlark.False,
		})

		args := starlark.Tuple{ictx}

		_, err := starlark.Call(&thread, f.install, args, nil)
		return h.Sum(nil), err
	}

	storePath, err := spec.Build(ctx, L, env, fn)
	if err != nil {
		return "", err
	}

	res.StorePath = storeDir
	res.AddResolution(f.Package.Name, storePath)

	return storePath, nil
}
