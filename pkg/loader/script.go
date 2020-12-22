package loader

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/chell"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/event"
	"github.com/lab47/chell/pkg/lang"
	"github.com/lab47/chell/pkg/metadata"
	"github.com/lab47/chell/pkg/repo"
	"github.com/lab47/chell/pkg/sumfile"
	"github.com/lab47/exprcore/exprcore"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
	"golang.org/x/crypto/blake2b"
)

type Loader struct {
	L           hclog.Logger
	cfg         *config.Config
	fns         exprcore.StringDict
	repo        repo.Repo
	constraints map[string]string

	mu      sync.Mutex
	scripts map[string]*Script
}

func NewLoader(L hclog.Logger, cfg *config.Config, r repo.Repo) (*Loader, error) {
	loader := &Loader{
		L:       L,
		fns:     lang.Funcs(),
		cfg:     cfg,
		repo:    r,
		scripts: make(map[string]*Script),
	}

	return loader, nil
}

type Script struct {
	loader *Loader
	name   string
	pkg    *exprcore.Prototype
	ent    repo.Entry

	helpers exprcore.StringDict

	signature string
}

func (s *Script) RepoId() string {
	return s.ent.RepoId()
}

func (s *Script) CarURL() string {
	return ""
}

func (s *Script) PackageProto() *exprcore.Prototype {
	return s.pkg
}

// String returns the string representation of the value.
// exprcore string values are quoted as if by Python's repr.
func (s *Script) String() string {
	return fmt.Sprintf(`script(name: "%s", package: %s)`, s.name, s.pkg)
}

// Type returns a short string describing the value's type.
func (s *Script) Type() string {
	return "chell.Script"
}

// Freeze causes the value, and all values transitively
// reachable from it through collections and closures, to be
// marked as frozen.  All subsequent mutations to the data
// structure through this API will fail dynamically, making the
// data structure immutable and safe for publishing to other
// exprcore interpreters running concurrently.
func (s *Script) Freeze() {
	return
}

// Truth returns the truth value of an object.
func (s *Script) Truth() exprcore.Bool {
	return exprcore.True
}

// Hash returns a function of x such that Equals(x, y) => Hash(x) == Hash(y).
// Hash may fail if the value's type is not hashable, or if the value
// contains a non-hashable value. The hash is used only by dictionaries and
// is not exposed to the exprcore program.
func (s *Script) Hash() (uint32, error) {
	h := fnv.New32()
	h.Write([]byte(s.name))
	return h.Sum32(), nil
}

func (s *Script) SaveSums(ctx context.Context) error {
	val, err := s.pkg.Attr("input")
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); ok {
			return nil
		}
		return err
	}

	sf, err := s.ent.Sumfile()
	if err != nil {
		sf = &sumfile.Sumfile{}
	}

	switch v := val.(type) {
	case *ScriptFile:
		if _, _, ok := sf.Lookup(v.path); !ok {
			algo, h, err := hashPath(ctx, v.path)
			if err != nil {
				return err
			}

			_, err = sf.Add(v.path, algo, h)
			if err != nil {
				return err
			}
		}
	case *exprcore.Dict:
		for _, dv := range v.Values() {
			if f, ok := dv.(*ScriptFile); ok {
				if _, _, ok := sf.Lookup(f.path); !ok {
					algo, h, err := hashPath(ctx, f.path)
					if err != nil {
						return err
					}

					hs, err := sf.Add(f.path, algo, h)
					if err != nil {
						return err
					}

					event.Fire(ctx, &event.HashedEvent{Entity: f.path, Hash: hs})
				}
			} else {
				return fmt.Errorf("unsupported type in inputs: %T", dv)
			}
		}
	default:
		return fmt.Errorf("unsupported type in inputs: %T", val)
	}

	return s.ent.SaveSumfile(sf)
}

const blakeAlgo = "b2"

func hashPath(ctx context.Context, path string) (string, []byte, error) {
	h, _ := blake2b.New256(nil)

	u, err := url.Parse(path)
	if err == nil {
		if u.Scheme == "http" || u.Scheme == "https" {
			event.Fire(ctx, &event.DownloadEvent{URL: path})

			resp, err := http.Get(path)
			if err != nil {
				return blakeAlgo, nil, err
			}

			defer resp.Body.Close()

			io.Copy(h, resp.Body)
			return blakeAlgo, h.Sum(nil), nil
		}

		return "", nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}

	defer f.Close()

	io.Copy(h, f)

	return blakeAlgo, h.Sum(nil), nil
}

func (s *Script) calculateSignature() error {
	h, _ := blake2b.New256(nil)

	var keys []string

	constraints := s.loader.cfg.Constraints()

	for _, k := range constraints {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, s.loader.constraints[k])
	}

	name, err := lang.StringValue(s.pkg.Attr("name"))
	if err != nil {
		return err
	}

	fmt.Fprintf(h, "%s\n", name)

	ver, err := lang.StringValue(s.pkg.Attr("version"))
	if err != nil {
		return err
	}

	fmt.Fprintf(h, "%s\n", ver)

	val, err := s.pkg.Attr("input")
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); ok {
			val = exprcore.None
		} else {
			return err
		}
	}

	if val != exprcore.None {
		sf, err := s.ent.Sumfile()
		if err != nil {
			sf = &sumfile.Sumfile{}
		}

		type hashVal struct {
			algo string
			h    []byte
		}

		var keys []string
		inputs := make(map[string]hashVal)

		switch v := val.(type) {
		case *ScriptFile:
			algo, h, ok := sf.Lookup(v.path)
			if !ok {
				return fmt.Errorf("missing sum for input: %s", v.path)
			}

			inputs[v.path] = hashVal{algo, h}
			keys = append(keys, v.path)
		case *exprcore.Dict:
			for _, dv := range v.Values() {
				if f, ok := dv.(*ScriptFile); ok {
					algo, h, ok := sf.Lookup(f.path)
					if !ok {
						return fmt.Errorf("missing sum for inputs: %s", f.path)
					}

					keys = append(keys, f.path)
					inputs[f.path] = hashVal{algo, h}
				} else {
					return fmt.Errorf("unsupported type in inputs: %T", dv)
				}
			}
		default:
			return fmt.Errorf("unsupported type in inputs: %T", val)
		}

		sort.Strings(keys)

		for _, k := range keys {
			fmt.Fprintf(h, "%s\n%s ", k, inputs[k].algo)
			h.Write(inputs[k].h)
		}
	}

	install, err := lang.FuncValue(s.pkg.Attr("install"))
	if err != nil {
		return err
	}

	hc, err := install.HashCode()
	if err != nil {
		return err
	}

	h.Write(hc)

	s.signature = base58.Encode(h.Sum(nil))

	return nil
}

func (s *Script) Signature() (string, error) {
	if s.signature == "" {
		err := s.calculateSignature()
		if err != nil {
			return "", err
		}
	}

	return s.signature, nil
}

func (s *Script) StoreName() (string, error) {
	sig, err := s.Signature()
	if err != nil {
		return "", err
	}

	name, err := lang.StringValue(s.pkg.Attr("name"))
	if err != nil {
		return "", err
	}

	ver, err := lang.StringValue(s.pkg.Attr("version"))
	if err != nil {
		return "", err
	}

	if ver == "" {
		ver = "unknown"
	}

	return fmt.Sprintf("%s-%s-%s", sig, name, ver), nil
}

func (s *Script) NameAndVersion() (string, string, error) {
	name, err := lang.StringValue(s.pkg.Attr("name"))
	if err != nil {
		return "", "", err
	}

	ver, err := lang.StringValue(s.pkg.Attr("version"))
	if err != nil {
		ver = "unknown"
	}

	return name, ver, nil
}

func (s *Script) EachInput(ctx context.Context, f func(name, path, algo string, hash []byte) error) error {
	val, err := s.pkg.Attr("input")
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); ok {
			return nil
		}
		return err
	}

	sf, err := s.ent.Sumfile()
	if err != nil {
		sf = &sumfile.Sumfile{}
	}

	switch v := val.(type) {
	case *ScriptFile:
		algo, hash, ok := sf.Lookup(v.path)
		if !ok {
			return fmt.Errorf("missing sum for input: %s", v.path)
		}

		err := f("source", v.path, algo, hash)
		if err != nil {
			return err
		}

	case *exprcore.Dict:
		sd := exprcore.StringDict{}
		v.ToStringDict(sd)
		for _, tup := range v.Items() {
			k := tup[0].(exprcore.String)
			dv := tup[1]

			if i, ok := dv.(*ScriptFile); ok {
				algo, hash, ok := sf.Lookup(i.path)
				if !ok {
					return fmt.Errorf("missing sum for input: %s", i.path)
				}

				err := f(string(k), i.path, algo, hash)
				if err != nil {
					return err
				}
			} else {
				return fmt.Errorf("unsupported type in inputs: %T", dv)
			}
		}
	default:
		return fmt.Errorf("unsupported type in inputs: %T", val)
	}

	return nil
}

func (l *Loader) loadHelpers(s *Script, vars exprcore.StringDict) error {
	exportName := s.name + ".export.chell"
	_, b, err := s.ent.Asset(exportName)
	if err != nil {
		return nil
	}

	isPD := vars.Has

	_, prog, err := exprcore.SourceProgram(exportName, b, isPD)
	if err != nil {
		return err
	}

	var thread exprcore.Thread

	thread.Import = l.importPkg
	thread.Shell = l.shell

	thread.SetLocal("install-env", &lang.InstallEnv{
		L: l.L,
	})

	gbls, _, err := prog.Init(&thread, vars)
	if err != nil {
		return err
	}

	s.helpers = gbls

	return nil
}

func (s *Script) Attr(name string) (exprcore.Value, error) {
	switch name {
	case "prefix":
		name, err := s.StoreName()
		if err != nil {
			return nil, err
		}

		return exprcore.String(filepath.Join(s.loader.cfg.DataDir, "store", name)), nil
	}

	if s.helpers == nil {
		return nil, nil
	}

	val, ok := s.helpers[name]
	if !ok {
		return nil, nil
	}

	return val, nil
}

func (s *Script) AttrNames() []string {
	if s.helpers == nil {
		return nil
	}

	return s.helpers.Keys()
}

var ErrBadScript = errors.New("script error detected")

func (l *Loader) LoadScript(name string) (*Script, error) {
	l.mu.Lock()
	script, ok := l.scripts[name]
	l.mu.Unlock()

	if ok {
		return script, nil
	}

	e, err := l.repo.Lookup(name)
	if err != nil {
		return nil, err
	}

	// vars := lang.Funcs()
	pkgobj := exprcore.FromStringDict(exprcore.Root, nil)

	vars := exprcore.StringDict{
		"pkg":    pkgobj,
		"file":   exprcore.NewBuiltin("file", l.fileFn),
		"inputs": exprcore.NewBuiltin("inputs", l.inputsFn),
	}

	/*
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
			"write_file":    exprcore.NewBuiltin("write_file", writeFileFn),
		}
	*/

	isPD := vars.Has

	fileName, data, err := e.Script()
	if err != nil {
		return nil, err
	}

	_, prog, err := exprcore.SourceProgram(fileName, data, isPD)
	if err != nil {
		return nil, err
	}

	// fn := &Function{}

	var thread exprcore.Thread

	thread.Import = l.importPkg
	thread.Shell = l.shell

	thread.SetLocal("install-env", &lang.InstallEnv{
		L: l.L,
		// storeDir: storeDir,
	})

	_, pkgval, err := prog.Init(&thread, vars)
	if err != nil {
		return nil, err
	}

	ppkg, ok := pkgval.(*exprcore.Prototype)
	if !ok {
		return nil, errors.Wrapf(ErrBadScript, "script did not return an object")
	}

	script = &Script{
		loader: l,
		name:   name,
		pkg:    ppkg,
		ent:    e,
	}

	l.mu.Lock()
	l.scripts[name] = script
	l.mu.Unlock()

	err = l.loadHelpers(script, vars)
	if err != nil {
		return nil, errors.Wrapf(ErrBadScript, "error loading helpers: %s", err)
	}

	return script, nil
}

func (l *Loader) importPkg(thread *exprcore.Thread, name string) (exprcore.Value, error) {
	s, err := l.LoadScript(name)
	return s, err
}

func (l *Loader) shell(thread *exprcore.Thread, parts []string) (exprcore.Value, error) {
	var (
		args       []string
		sb         strings.Builder
		inside     bool
		insideChar rune
	)

	for _, p := range parts {
		for _, r := range p {
			switch r {
			case ' ', '\t', '\n', '\r':
				if !inside {
					if sb.Len() > 0 {
						args = append(args, sb.String())
						sb.Reset()
					}
					continue
				}
			case '"', '\'':
				if !inside {
					inside = true
					insideChar = r
					continue
				} else if insideChar == r {
					inside = false
					continue
				}
			}

			sb.WriteRune(r)
		}
	}

	if sb.Len() > 0 {
		args = append(args, sb.String())
	}

	spew.Dump(args)

	return exprcore.None, nil
}

func (s *Script) Install() (*exprcore.Function, error) {
	return lang.FuncValue(s.pkg.Attr("install"))
}

func (s *Script) Hook() (*exprcore.Function, error) {
	return lang.FuncValue(s.pkg.Attr("hook"))
}

func (s *Script) Dependencies() ([]*Script, error) {
	deps, err := lang.ListValue(s.pkg.Attr("dependencies"))
	if err != nil {
		return nil, err
	}

	if deps == nil {
		return nil, nil
	}

	var scripts []*Script

	iter := deps.Iterate()
	defer iter.Done()
	var x exprcore.Value
	for iter.Next(&x) {
		if script, ok := x.(*Script); ok {
			scripts = append(scripts, script)
		}
	}

	return scripts, nil
}

func (s *Script) gatherDependencies(seen map[*Script]struct{}) ([]*Script, error) {
	if seen == nil {
		seen = map[*Script]struct{}{}
	}

	deps, err := s.Dependencies()
	if err != nil {
		return nil, err
	}

	var scripts []*Script

	for _, dep := range deps {
		if _, ok := seen[dep]; ok {
			continue
		}

		seen[dep] = struct{}{}

		scripts = append(scripts, dep)

		sub, err := dep.gatherDependencies(seen)
		if err != nil {
			return nil, err
		}

		scripts = append(scripts, sub...)
	}

	return scripts, nil
}

func (s *Script) Env(storeDir string) (map[string]string, error) {
	name, err := s.StoreName()
	if err != nil {
		return nil, err
	}

	var info metadata.InstallInfo

	f, err := os.Open(filepath.Join(storeDir, name+".json"))
	if err != nil {
		return nil, err
	}

	err = json.NewDecoder(f).Decode(&info)
	if err != nil {
		return nil, err
	}

	path := []string{}

	bin := filepath.Join(storeDir, name, "bin")

	if fi, err := os.Stat(bin); err == nil && fi.IsDir() {
		path = append(path, bin)
	}

	for _, dep := range info.Dependencies {
		bin := filepath.Join(storeDir, dep.Id, "bin")

		if fi, err := os.Stat(bin); err == nil && fi.IsDir() {
			path = append(path, bin)
		}
	}

	env := map[string]string{
		"PATH": strings.Join(path, string(filepath.ListSeparator)),
	}

	return env, nil
}

type Package struct {
	chell.Package

	install *exprcore.Function
	hook    *exprcore.Function
}

func (s *Script) Package() (*Package, error) {
	var (
		pkg Package
		err error
	)

	pkg.install, err = lang.FuncValue(s.pkg.Attr("install"))
	if err != nil {
		return nil, err
	}

	pkg.hook, err = lang.FuncValue(s.pkg.Attr("hook"))
	if err != nil {
		return nil, err
	}

	pkg.Name, err = lang.StringValue(s.pkg.Attr("name"))
	if err != nil {
		return nil, err
	}

	pkg.Source, err = lang.StringValue(s.pkg.Attr("source"))
	if err != nil {
		return nil, err
	}

	pkg.Version, err = lang.StringValue(s.pkg.Attr("version"))
	if err != nil {
		return nil, err
	}

	pkg.Sha256, err = lang.StringValue(s.pkg.Attr("sha256"))
	if err != nil {
		return nil, err
	}

	deps, err := lang.ListValue(s.pkg.Attr("dependencies"))
	if err != nil {
		return nil, err
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

	return &pkg, nil
}

func (l *Loader) LoadDeps(p *Package) (map[string]*Package, error) {
	deps := map[string]*Package{}

	for _, dep := range p.Deps.Runtime {
		script, err := l.LoadScript(dep)
		if err != nil {
			return nil, err
		}

		pkg, err := script.Package()
		if err != nil {
			return nil, err
		}

		deps[dep] = pkg
	}

	return deps, nil
}

type ScriptFile struct {
	script *Script
	path   string

	helpers exprcore.StringDict
}

// String returns the string representation of the value.
// exprcore string values are quoted as if by Python's repr.
func (s *ScriptFile) String() string {
	return fmt.Sprintf("file(path: %s)", s.path)
}

// Type returns a short string describing the value's type.
func (s *ScriptFile) Type() string {
	return "script:file"
}

func (s *ScriptFile) Freeze() {}

func (s *ScriptFile) Truth() exprcore.Bool {
	return exprcore.True
}

func (s *ScriptFile) Hash() (uint32, error) {
	h := fnv.New32()
	h.Write([]byte(s.path))
	return h.Sum32(), nil
}

type assetEntry struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
}

func (s *ScriptFile) ExpectedHash() ([]byte, error) {
	_, data, err := s.script.ent.Asset("assets.json")
	if err != nil {
		return nil, nil
	}

	var entries []assetEntry

	err = json.Unmarshal(data, &entries)
	if err != nil {
		return nil, err
	}

	for _, ent := range entries {
		if ent.Path == s.path {
			return base64.StdEncoding.DecodeString(ent.Sha256)
		}
	}

	return nil, nil
}

func (s *ScriptFile) CurrentHash() ([]byte, error) {
	_, err := url.Parse(s.path)
	if err != nil {
		f, err := os.Open(s.path)
		if err != nil {
			return nil, err
		}

		h := sha256.New()
		io.Copy(h, f)

		return h.Sum(nil), nil
	}

	r, err := http.Get(s.path)
	if err != nil {
		return nil, err
	}

	defer r.Body.Close()

	h := sha256.New()
	io.Copy(h, r.Body)

	return h.Sum(nil), nil
}

type Asset interface {
	ExpectedHash() ([]byte, error)
	CurrentHash() ([]byte, error)
	Open() (io.ReadCloser, error)
}

func (l *Loader) fileFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var (
		path, darwin, linux string
	)

	if err := exprcore.UnpackArgs(
		"file", args, kwargs,
		"path?", &path,
		"darwin?", &darwin,
		"linux?", &linux,
	); err != nil {
		return nil, err
	}

	if path == "" {
		switch runtime.GOOS {
		case "darwin":
			if darwin != "" {
				path = darwin
			}
		case "linux":
			if linux != "" {
				path = linux
			}
		}
	}

	return &ScriptFile{path: path}, nil
}

func (l *Loader) inputsFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {

	sm := exprcore.NewDict(len(kwargs))

	for _, ent := range kwargs {
		sm.SetKey(ent[0], ent[1])
	}

	return sm, nil
}
