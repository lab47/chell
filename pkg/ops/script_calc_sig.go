package ops

import (
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/hashicorp/go-hclog"
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
	common

	Name         string
	Version      string
	Install      *exprcore.Function
	Hook         *exprcore.Function
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

	hook, err := lang.FuncValue(proto.Attr("hook"))
	if err != nil {
		return err
	}

	s.Hook = hook

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

type calcLogger struct {
	logger hclog.Logger
	h      hash.Hash
}

func (c *calcLogger) Write(b []byte) (int, error) {
	c.h.Write(b)

	s := strconv.QuoteToASCII(string(b))

	/*
		for _, r := range s {
			if !unicode.IsPrint(r) {
				c.logger.Debug("calc-part", "part", b)
				return len(b), nil
			}
		}
	*/

	c.logger.Debug("calc-part", "part", s[1:len(s)-1], "sum", hex.EncodeToString(c.h.Sum(nil)))
	return len(b), nil
}

func (s *ScriptCalcSig) calcSig(
	proto *exprcore.Prototype,
	data ScriptData,
	helperSum []byte,
	constraints map[string]string,
) (string, error) {
	if s.Name == "" {
		err := s.extract(proto)
		if err != nil {
			return "", err
		}
	}

	hb, _ := blake2b.New256(nil)

	h := &calcLogger{logger: s.L(), h: hb}

	fmt.Fprintf(h, "name: %s\n", s.Name)

	fmt.Fprintf(h, "version: %s\n", s.Version)

	var keys []string

	for k := range constraints {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(h, "constraint %s = %s\n", k, constraints[k])
	}

	if s.Inputs != nil {
		err := s.injectInputs(h, data)
		if err != nil {
			return "", err
		}
	}

	if s.Install != nil {
		err := s.callAsCalc(h)
		if err != nil {
			return "", err
		}
	}

	var depIds []string

	for _, scr := range s.Dependencies {
		depIds = append(depIds, scr.ID())
	}

	sort.Strings(depIds)

	for _, id := range depIds {
		fmt.Fprintf(h, "dep: %s\n", id)
	}

	return base58.Encode(hb.Sum(nil)), nil
}

func (s *ScriptCalcSig) callAsCalc(h io.Writer) error {
	var rc RunCtx
	rc.attrs = RunCtxFunctions

	args := exprcore.Tuple{&rc}

	var thread exprcore.Thread

	thread.CallTrace = func(thread *exprcore.Thread, c exprcore.Callable, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
		switch v := c.(type) {
		case *exprcore.Builtin:
			rec := v.Receiver()
			if rec == nil {
				fmt.Printf("+ %s", v.Name())
			} else {
				fmt.Printf("+ %s.%s", rec.String(), v.Name())
			}
		case *exprcore.Function:
			fmt.Printf("+ %s", v.Name())
		default:
			fmt.Printf("+ %#v", v)
		}

		fmt.Printf("(%s, ", args)

		for _, p := range kwargs {
			fmt.Printf("[%s] ", p.String())
		}

		fmt.Printf(")\n")

		return nil, exprcore.CallContinue
	}

	fmt.Fprintf(h, "install fn\n")

	rc.h = h

	_, err := exprcore.Call(&thread, s.Install, args, nil)
	if err != nil {
		return err
	}

	return nil
}

func (s *ScriptCalcSig) injectInputs(w io.Writer, data ScriptData) error {
	for _, i := range s.Inputs {
		algo, h, ok := s.hashPath(i.Data, data)
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

func (s *ScriptCalcSig) hashPath(sf *ScriptFile, data ScriptData) (string, []byte, bool) {
	path := sf.path

	if kt, kv, ok := sf.Sum(); ok {
		return kt, kv, true
	}

	h, _ := blake2b.New256(nil)

	u, err := url.Parse(path)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		resp, err := http.Head(path)
		if err != nil {
			return "", nil, false
		}

		defer resp.Body.Close()

		if etag := resp.Header.Get("Etag"); etag != "" && etag[0] == '"' {
			return "etag", []byte(etag), true
		}

		resp, err = http.Get(path)
		if err != nil {
			return "", nil, false
		}

		defer resp.Body.Close()

		io.Copy(h, resp.Body)
	} else {
		ad, err := data.Asset(path)
		if err != nil {
			return "", nil, false
		}

		_, err = h.Write(ad)
		if err != nil {
			return "", nil, false
		}
	}

	return "b2", h.Sum(nil), true
}

var times int

func (s *ScriptCalcSig) Calculate(
	proto *exprcore.Prototype,
	data ScriptData,
	helperSum []byte,
	constraints map[string]string,
) (string, string, error) {
	sig, err := s.calcSig(proto, data, helperSum, constraints)
	if err != nil {
		return "", "", err
	}

	return sig, fmt.Sprintf("%s-%s-%s", sig, s.Name, s.Version), nil
}
