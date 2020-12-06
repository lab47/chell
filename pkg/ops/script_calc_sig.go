package ops

import (
	"fmt"
	"io"
	"sort"

	"github.com/lab47/chell/pkg/lang"
	"github.com/lab47/exprcore/exprcore"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
)

type ScriptInput struct {
	Name string
	Data *ScriptFile
}

type ScriptCalcSig struct {
	Name         string
	Version      string
	Install      *exprcore.Function
	Inputs       []ScriptInput
	Dependencies []*ScriptPackage
}

func (s *ScriptCalcSig) extract(proto *exprcore.Prototype) error {
	name, err := lang.StringValue(proto.Attr("name"))
	if err != nil {
		return err
	}

	s.Name = name

	ver, err := lang.StringValue(proto.Attr("version"))
	if err != nil {
		return err
	}

	if ver == "" {
		ver = "unknown"
	}

	s.Version = ver

	val, err := proto.Attr("input")
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); ok {
			val = nil
		} else {
			return err
		}
	}

	if val != nil {
		err = s.processInput(val)
		if err != nil {
			return err
		}
	}

	install, err := lang.FuncValue(proto.Attr("install"))
	if err != nil {
		return err
	}

	s.Install = install

	deps, err := lang.ListValue(proto.Attr("dependencies"))
	if err != nil {
		return err
	}

	if deps != nil {
		var scripts []*ScriptPackage

		iter := deps.Iterate()
		defer iter.Done()
		var x exprcore.Value
		for iter.Next(&x) {
			if script, ok := x.(*ScriptPackage); ok {
				scripts = append(scripts, script)
			}
		}

		s.Dependencies = scripts
	}

	return nil
}

func (s *ScriptCalcSig) processInput(val exprcore.Value) error {
	var inputs []ScriptInput

	switch v := val.(type) {
	case *ScriptFile:
		inputs = append(inputs, ScriptInput{
			Name: "source",
			Data: v,
		})
	case *exprcore.Dict:
		for _, i := range v.Items() {
			key, ok := i.Index(0).(exprcore.String)
			if !ok {
				return fmt.Errorf("key not a string")
			}

			dv := i.Index(1)

			if f, ok := dv.(*ScriptFile); ok {
				inputs = append(inputs, ScriptInput{
					Name: string(key),
					Data: f,
				})
			} else {
				return fmt.Errorf("unsupported type in inputs: %T", dv)
			}
		}
	default:
		return fmt.Errorf("unsupported type in inputs: %T", val)
	}

	sort.Slice(inputs, func(i, j int) bool {
		return inputs[i].Name < inputs[j].Name
	})

	s.Inputs = inputs

	return nil
}

func (s *ScriptCalcSig) calcSig(proto *exprcore.Prototype, data ScriptData) (string, error) {
	if s.Name == "" {
		err := s.extract(proto)
		if err != nil {
			return "", err
		}
	}

	h, _ := blake2b.New256(nil)
	fmt.Fprintf(h, "name: %s\n", s.Name)

	fmt.Fprintf(h, "version: %s\n", s.Version)

	if s.Inputs != nil {
		err := s.injectInputs(h, data)
		if err != nil {
			return "", err
		}
	}

	if s.Install != nil {
		codeHash, err := s.Install.HashCode()
		if err != nil {
			return "", err
		}

		h.Write(codeHash)
	}

	var depIds []string

	for _, scr := range s.Dependencies {
		depIds = append(depIds, scr.ID())
	}

	sort.Strings(depIds)

	for _, id := range depIds {
		fmt.Fprintf(h, "dep: %s\n", id)
	}

	return base58.Encode(h.Sum(nil)), nil
}

func (s *ScriptCalcSig) injectInputs(w io.Writer, data ScriptData) error {
	for _, i := range s.Inputs {
		algo, h, ok := s.hashPath(i.Data.path, data)
		if !ok {
			return fmt.Errorf("missing sum for input: %s", i.Data.path)
		}

		fmt.Fprintf(w, "path: %s\nalgo: %s\n", i.Data.path, algo)
		_, err := w.Write(h)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *ScriptCalcSig) hashPath(path string, data ScriptData) (string, []byte, bool) {
	h, _ := blake2b.New256(nil)

	ad, err := data.Asset(path)
	if err != nil {
		return "", nil, false
	}

	_, err = h.Write(ad)
	if err != nil {
		return "", nil, false
	}

	return "b2", h.Sum(nil), true
}

func (s *ScriptCalcSig) Calculate(proto *exprcore.Prototype, data ScriptData) (string, error) {
	sig, err := s.calcSig(proto, data)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s-%s-%s", sig, s.Name, s.Version), nil
}
