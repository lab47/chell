package ops

import (
	"fmt"
	"hash/fnv"
	"io"
	"runtime"

	"github.com/lab47/exprcore/exprcore"
	"github.com/pkg/errors"
)

type ScriptLoad struct {
	lookup *ScriptLookup

	loaded map[string]*ScriptPackage
}

type ScriptPackage struct {
	id        string
	prototype *exprcore.Prototype

	cs ScriptCalcSig

	helpers exprcore.StringDict
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

var ErrBadScript = errors.New("script error detected")

func (s *ScriptLoad) Load(name string) (*ScriptPackage, error) {
	if s.loaded == nil {
		s.loaded = make(map[string]*ScriptPackage)
	}

	sp, ok := s.loaded[name]
	if ok {
		return sp, nil
	}

	data, err := s.lookup.Load(name)
	if err != nil {
		return nil, err
	}

	pkgobj := exprcore.FromStringDict(exprcore.Root, nil)

	vars := exprcore.StringDict{
		"pkg":    pkgobj,
		"file":   exprcore.NewBuiltin("file", s.fileFn),
		"inputs": exprcore.NewBuiltin("inputs", s.inputsFn),
	}

	_, prog, err := exprcore.SourceProgram(name+".chell", data.Script(), vars.Has)
	if err != nil {
		return nil, err
	}

	var thread exprcore.Thread

	thread.Import = s.importPkg

	_, pkgval, err := prog.Init(&thread, vars)
	if err != nil {
		return nil, err
	}

	ppkg, ok := pkgval.(*exprcore.Prototype)
	if !ok {
		return nil, errors.Wrapf(ErrBadScript, "script did not return an object")
	}

	sp = &ScriptPackage{}

	id, err := sp.cs.Calculate(ppkg, data)
	if err != nil {
		return nil, err
	}

	sp.id = id
	sp.prototype = ppkg

	err = s.loadHelpers(sp, name, data, vars)
	if err != nil {
		return nil, err
	}

	s.loaded[name] = sp

	return sp, nil
}

func (s *ScriptLoad) importPkg(thread *exprcore.Thread, name string) (exprcore.Value, error) {
	x, err := s.Load(name)
	return x, err
}

func (l *ScriptLoad) fileFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
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

func (l *ScriptLoad) inputsFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {

	sm := exprcore.NewDict(len(kwargs))

	for _, ent := range kwargs {
		sm.SetKey(ent[0], ent[1])
	}

	return sm, nil
}

type ScriptFile struct {
	path string
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
	exportName := name + ".helpers.chell"
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
	/*
		switch name {
		case "prefix":
			name := s.ID()
			return exprcore.String(filepath.Join(s.loader.cfg.DataDir, "store", name)), nil
		}
	*/

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
