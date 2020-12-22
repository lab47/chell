package ops

import (
	"fmt"
	"hash/fnv"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/lab47/exprcore/exprcore"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
)

type ScriptLoad struct {
	StoreDir string

	lookup *ScriptLookup

	loaded map[string]*ScriptPackage
}

func loadedKey(name string, args map[string]string) string {
	var keys []string

	for k := range args {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(name)

	if len(keys) > 1 {
		sb.WriteByte('-')

		for _, k := range keys {
			sb.WriteString(k)
			sb.WriteByte('=')
			sb.WriteString(args[k])
		}
	}

	return sb.String()
}

type ScriptPackage struct {
	loader *ScriptLoad

	id        string
	repo      string
	prototype *exprcore.Prototype

	cs ScriptCalcSig

	helpers exprcore.StringDict

	constraints map[string]string
}

// String returns the string representation of the value.
// exprcore string values are quoted as if by Python's repr.
func (s *ScriptPackage) String() string {
	return "<script>"
}

// Type returns a short string describing the value's type.
func (s *ScriptPackage) Type() string {
	return "script"
}

// Freeze causes the value, and all values transitively
// reachable from it through collections and closures, to be
// marked as frozen.  All subsequent mutations to the data
// structure through this API will fail dynamically, making the
// data structure immutable and safe for publishing to other
// exprcore interpreters running concurrently.
func (s *ScriptPackage) Freeze() {
}

// Truth returns the truth value of an object.
func (s *ScriptPackage) Truth() exprcore.Bool {
	return exprcore.True
}

// Hash returns a function of x such that Equals(x, y) => Hash(x) == Hash(y).
// Hash may fail if the value's type is not hashable, or if the value
// contains a non-hashable value. The hash is used only by dictionaries and
// is not exposed to the exprcore program.
func (s *ScriptPackage) Hash() (uint32, error) {
	return 0, io.EOF
}

func (s *ScriptPackage) ID() string {
	return s.id
}

func (s *ScriptPackage) Repo() string {
	return s.repo
}

func (s *ScriptPackage) Constraints() map[string]string {
	return s.constraints
}

// Dependencies returns any ScriptPackages that this one depends on, as
// declared via the dependencies keyword.
func (s *ScriptPackage) Dependencies() []*ScriptPackage {
	return s.cs.Dependencies
}

var ErrBadScript = errors.New("script error detected")

type Option func(c *loadCfg)

type loadCfg struct {
	args, constraints map[string]string
}

func WithArgs(args map[string]string) Option {
	return func(c *loadCfg) {
		c.args = args
	}
}

func WithConstraints(args map[string]string) Option {
	return func(c *loadCfg) {
		c.constraints = args
	}
}

func (s *ScriptLoad) Load(name string, opts ...Option) (*ScriptPackage, error) {
	if s.loaded == nil {
		s.loaded = make(map[string]*ScriptPackage)
	}

	var lc loadCfg

	for _, o := range opts {
		o(&lc)
	}

	cacheKey := loadedKey(name, lc.args)

	sp, ok := s.loaded[cacheKey]
	if ok {
		return sp, nil
	}

	data, err := s.lookup.Load(name)
	if err != nil {
		return nil, err
	}

	pkgobj := exprcore.FromStringDict(exprcore.Root, nil)

	args := exprcore.NewDict(len(lc.args))

	for k, v := range lc.args {
		args.SetKey(exprcore.String(k), exprcore.String(v))
	}

	vars := exprcore.StringDict{
		"pkg":    pkgobj,
		"args":   args,
		"file":   exprcore.NewBuiltin("file", s.fileFn),
		"inputs": exprcore.NewBuiltin("inputs", s.inputsFn),
	}

	_, prog, err := exprcore.SourceProgram(name+".chell", data.Script(), vars.Has)
	if err != nil {
		return nil, err
	}

	var thread exprcore.Thread

	thread.Import = s.importPkg

	thread.SetLocal("constraints", lc.constraints)

	_, pkgval, err := prog.Init(&thread, vars)
	if err != nil {
		return nil, err
	}

	ppkg, ok := pkgval.(*exprcore.Prototype)
	if !ok {
		return nil, errors.Wrapf(ErrBadScript, "script did not return an object")
	}

	sp = &ScriptPackage{
		loader:      s,
		constraints: lc.constraints,
	}

	id, err := sp.cs.Calculate(ppkg, data, lc.constraints)
	if err != nil {
		return nil, err
	}

	sp.id = id
	sp.prototype = ppkg

	s.loaded[cacheKey] = sp

	err = s.loadHelpers(sp, name, data, vars)
	if err != nil {
		return nil, err
	}

	return sp, nil
}

func (s *ScriptLoad) importPkg(thread *exprcore.Thread, name string) (exprcore.Value, error) {
	var opts []Option

	constraints := thread.Local("constraints")
	if constraints != nil {
		opts = append(opts, WithConstraints(constraints.(map[string]string)))
	}

	x, err := s.Load(name, opts...)
	return x, err
}

var ErrSumFormat = fmt.Errorf("sum must a tuple with (sum-type, sum)")

func (l *ScriptLoad) fileFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var (
		path, darwin, linux string
		sum                 exprcore.Tuple
	)

	if err := exprcore.UnpackArgs(
		"file", args, kwargs,
		"path?", &path,
		"sum?", &sum,
		"darwin?", &darwin,
		"linux?", &linux,
	); err != nil {
		return nil, err
	}

	var sumType, sumVal exprcore.String

	if sum != nil {
		if len(sum) != 2 {
			return nil, ErrSumFormat
		}

		var ok bool

		sumType, ok = sum[0].(exprcore.String)
		if !ok {
			return nil, ErrSumFormat
		}

		sumVal, ok = sum[1].(exprcore.String)
		if !ok {
			return nil, ErrSumFormat
		}
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

	return &ScriptFile{
		path:     path,
		sumType:  string(sumType),
		sumValue: string(sumVal),
	}, nil
}

func (l *ScriptLoad) inputsFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {

	sm := exprcore.NewDict(len(kwargs))

	for _, ent := range kwargs {
		sm.SetKey(ent[0], ent[1])
	}

	return sm, nil
}

type ScriptFile struct {
	path     string
	sumType  string
	sumValue string
}

func (s *ScriptFile) Sum() (string, []byte, bool) {
	switch s.sumType {
	case "etag":
		if len(s.sumValue) < 2 {
			return "", nil, false
		}

		sv := s.sumValue
		if sv[0] != '"' {
			sv = "\"" + sv
		}

		if sv[len(sv)-1] != '"' {
			sv = sv + "\""
		}

		return "etag", []byte(sv), true
	default:
		b, err := base58.Decode(s.sumValue)
		if err != nil {
			return "", nil, false
		}

		return s.sumType, b, true
	}
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

func (l *ScriptLoad) loadHelpers(s *ScriptPackage, name string, data ScriptData, vars exprcore.StringDict) error {
	exportName := name + ".export.chell"
	b, err := data.Asset(exportName)
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

	gbls, _, err := prog.Init(&thread, vars)
	if err != nil {
		return err
	}

	s.helpers = gbls

	return nil
}

func (s *ScriptPackage) Attr(name string) (exprcore.Value, error) {
	switch name {
	case "prefix":
		return exprcore.String(filepath.Join(s.loader.StoreDir, s.ID())), nil
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

func (s *ScriptPackage) AttrNames() []string {
	if s.helpers == nil {
		return nil
	}

	return s.helpers.Keys()
}
