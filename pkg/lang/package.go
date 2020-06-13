package lang

import (
	"bytes"
	"context"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanphx/chell/pkg/builder"
	"github.com/evanphx/chell/pkg/chell"
	"github.com/evanphx/chell/pkg/resolver"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/exprcore/exprcore"
	"github.com/lab47/exprcore/exprcorestruct"
	"github.com/mitchellh/hashstructure"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
)

type Function struct {
	Code string

	Package *chell.Package
	install *exprcore.Function
	hook    *exprcore.Function

	installDir string
	buildDir   string

	Dependencies []*Function

	hash []byte
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
	L hclog.Logger

	installDir, buildDir, storeDir string
	extraEnv                       []string

	h        hash.Hash
	hashOnly bool

	outputPrefix string
}

type PackageValue struct {
	chell.Package

	install *exprcore.Function

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
// exprcore interpreters running concurrently.
func (p *PackageValue) Freeze() {
	p.frozen = true
}

// Truth returns the truth value of an object.
func (p *PackageValue) Truth() exprcore.Bool {
	return exprcore.True
}

// Hash returns a function of x such that Equals(x, y) => Hash(x) == Hash(y).
// Hash may fail if the value's type is not hashable, or if the value
// contains a non-hashable value. The hash is used only by dictionaries and
// is not exposed to the exprcore program.
func (p *PackageValue) Hash() (uint32, error) {
	h, err := hashstructure.Hash(p, nil)
	if err != nil {
		return 0, err
	}

	return uint32(h), nil
}

func Locate(L hclog.Logger, path, storeDir, pkgPath string) (*Function, error) {
	for _, dir := range filepath.SplitList(pkgPath) {
		tp := filepath.Join(dir, path) + ".chell"

		if _, err := os.Stat(tp); err == nil {
			return Load(L, tp, storeDir, pkgPath)
		}
	}

	return nil, fmt.Errorf("unable to locate package definition: %s", path)
}

func Load(L hclog.Logger, path, storeDir, pkgPath string) (*Function, error) {
	vars := makeFuncs()

	isPD := vars.Has

	_, prog, err := exprcore.SourceProgram(path, nil, isPD)
	if err != nil {
		return nil, err
	}

	fn := &Function{}

	var thread exprcore.Thread

	thread.SetLocal("install-env", &installEnv{
		L:        L,
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

	if pkg.install != nil {
		fn.install = pkg.install
	} else {
		if f, ok := glb["install"].(*exprcore.Function); ok {
			fn.install = f
		}
	}

	if f, ok := glb["hook"].(*exprcore.Function); ok {
		fn.hook = f
	}

	for _, dep := range fn.Package.Deps.Runtime {
		sub, err := Locate(L, dep, storeDir, pkgPath)
		if err != nil {
			return nil, err
		}

		fn.Dependencies = append(fn.Dependencies, sub)
	}

	return fn, nil
}

const FakePath = "/non-existant"

func (f *Function) HashInstall(ctx context.Context) ([]byte, error) {
	if f.hash != nil {
		return f.hash, nil
	}

	h, _ := blake2b.New(16, nil)

	// hash the dependencies sorted

	var (
		depKeys []string
	)

	deps := map[string]*Function{}
	for _, dep := range f.Dependencies {
		depKeys = append(depKeys, dep.Package.Name)
		deps[dep.Package.Name] = dep
	}

	sort.Strings(depKeys)

	pkgs := exprcore.StringDict{}

	for _, k := range depKeys {
		dep := deps[k]

		dh, err := dep.HashInstall(ctx)
		if err != nil {
			return nil, err
		}

		fmt.Fprintf(h, "dep:%s", k)
		h.Write(dh)

		name, err := dep.StoreName(ctx)
		if err != nil {
			return nil, err
		}

		pkgs[k] = exprcore.String(filepath.Join(FakePath, name))
	}

	fmt.Fprintln(h, "host-path=/bin:/usr/bin")

	var thread exprcore.Thread

	L := hclog.FromContext(ctx)

	thread.SetLocal("install-env", &installEnv{
		L:        L,
		buildDir: FakePath,
		storeDir: FakePath,
		h:        h,
		hashOnly: true,
	})

	ictx := exprcorestruct.FromStringDict(exprcorestruct.Default, exprcore.StringDict{
		"prefix":  exprcore.String("/tmp"),
		"build":   exprcore.String(FakePath),
		"pkgs":    exprcorestruct.FromStringDict(exprcorestruct.Default, pkgs),
		"head_eh": exprcore.False,
	})

	args := exprcore.Tuple{ictx}

	_, err := exprcore.Call(&thread, f.install, args, nil)
	if err != nil {
		return nil, err
	}

	f.hash = h.Sum(nil)

	return f.hash, nil
}

func (f *Function) StoreName(ctx context.Context) (string, error) {
	ih, err := f.HashInstall(ctx)
	if err != nil {
		return "", err
	}

	h, _ := blake2b.New(16, nil)

	fmt.Fprintln(h, f.Package.Name)
	fmt.Fprintln(h, f.Package.Version)
	fmt.Fprintln(h, f.Package.Sha256)

	h.Write(ih)

	return fmt.Sprintf("%s-%s-%s", base58.Encode(h.Sum(nil)), f.Package.Name, f.Package.Version), nil
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

	storeName, err := f.StoreName(ctx)
	if err != nil {
		return "", err
	}

	spec := &builder.Spec{
		StoreName: storeName,
		Name:      f.Package.Name,
		Version:   f.Package.Version,
		Source:    f.Package.Source,
	}

	pkgs := exprcore.StringDict{}

	for _, dep := range f.Package.Deps.Runtime {
		sp, err := res.Resolve(dep)
		if err != nil {
			return "", err
		}

		spec.Dependencies = append(spec.Dependencies, sp)

		pkgs[dep] = exprcore.String(filepath.Join(storeDir, sp))
	}

	os.MkdirAll(buildDir, 0755)

	env := &builder.Env{
		BuildDir: buildDir,
		StoreDir: storeDir,
	}

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
		var thread exprcore.Thread

		h, _ := blake2b.New(16, nil)

		thread.SetLocal("install-env", &installEnv{
			L:            L,
			buildDir:     buildDir,
			installDir:   installDir,
			storeDir:     storeDir,
			extraEnv:     append(buildEnv, "HOME=/nonexistant", "PATH="+path),
			h:            h,
			outputPrefix: f.Package.Name,
		})

		thread.SetLocal("resolver", res)

		for i, dep := range f.Dependencies {
			L.Trace("consider hook", "pkg", dep.Package.Name, "hook", dep.hook)
			if dep.hook == nil {
				continue
			}

			ictx := exprcorestruct.FromStringDict(exprcorestruct.Default, exprcore.StringDict{
				"prefix": exprcore.String(filepath.Join(storeDir, spec.Dependencies[i])),
				"build":  exprcore.String(buildDir),
			})

			args := exprcore.Tuple{ictx}

			_, err := exprcore.Call(&thread, dep.hook, args, nil)
			if err != nil {
				return h.Sum(nil), err
			}
		}

		ictx := exprcorestruct.FromStringDict(exprcorestruct.Default, exprcore.StringDict{
			"prefix":  exprcore.String(installDir),
			"build":   exprcore.String(buildDir),
			"pkgs":    exprcorestruct.FromStringDict(exprcorestruct.Default, pkgs),
			"head_eh": exprcore.False,
		})

		args := exprcore.Tuple{ictx}

		L.Info("building package", "name", f.Package.Name, "version", f.Package.Version, "store-name", storeName)
		_, err := exprcore.Call(&thread, f.install, args, nil)
		return nil, err
	}

	storePath, err := spec.Build(ctx, L, env, fn)
	if err != nil {
		return "", err
	}

	res.StorePath = storeDir
	res.AddResolution(f.Package.Name, storePath)

	return storePath, nil
}
