package ops

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lab47/chell/pkg/data"
	"github.com/lab47/chell/pkg/lang"
	"github.com/lab47/exprcore/exprcore"
)

type ConfigRepo struct {
	Github string
	Path   string
}

type Config struct {
	Repos map[string]*ConfigRepo
}

type ConfigLoad struct {
	load        *ScriptLoad
	constraints map[string]string

	cfg Config

	toInstall []*ScriptPackage
}

var repotype = exprcore.FromStringDict(exprcore.Root, nil)

func (c *ConfigLoad) LoadConfig(root string) (*Config, error) {
	var cfg Config
	var srcs data.Sources

	cfg.Repos = make(map[string]*ConfigRepo)

	// TODO load gloabl info and merge it with sources

	f, err := os.Open(filepath.Join(root, "sources.json"))
	if err != nil {
		return &cfg, nil
	}

	err = json.NewDecoder(f).Decode(&srcs)
	if err != nil {
		return nil, err
	}

	var only string

	for k, ref := range srcs {
		only = k
		cfg.Repos[k] = &ConfigRepo{
			Path: ref,
		}
	}

	if len(cfg.Repos) == 1 {
		cfg.Repos["root"] = cfg.Repos[only]
	}

	return &cfg, nil
}

type Project struct {
	ToInstall []*ScriptPackage
}

func (c *ConfigLoad) LoadScript(r io.Reader, opts ...Option) (*Project, error) {
	var lc loadCfg

	for _, o := range opts {
		o(&lc)
	}

	c.constraints = lc.constraints

	vars := exprcore.StringDict{
		"install": exprcore.NewBuiltin("install", c.installFn),
	}

	_, prog, err := exprcore.SourceProgram("config.chell", r, vars.Has)
	if err != nil {
		return nil, err
	}

	var thread exprcore.Thread

	thread.Import = c.importPkg

	_, _, err = prog.Init(&thread, vars)
	if err != nil {
		return nil, err
	}

	var proj Project
	proj.ToInstall = c.toInstall

	return &proj, nil
}

func (l *ConfigLoad) installFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	for _, arg := range args {
		if sp, ok := arg.(*ScriptPackage); ok {
			l.toInstall = append(l.toInstall, sp)
		}
	}

	return exprcore.None, nil
}

func (s *ConfigLoad) importPkg(thread *exprcore.Thread, ns, name string, args *exprcore.Dict) (exprcore.Value, error) {
	var opts []Option

	constraints := s.constraints
	if constraints != nil {
		opts = append(opts, WithConstraints(constraints))
	}

	if args != nil {
		loadArgs := make(map[string]string)

		for _, pair := range args.Items() {
			k, ok := pair[0].(exprcore.String)
			if !ok {
				return nil, fmt.Errorf("load arg key not a string")
			}

			v, ok := pair[1].(exprcore.String)
			if !ok {
				return nil, fmt.Errorf("load arg value not a string")
			}

			loadArgs[string(k)] = string(v)
		}

		opts = append(opts, WithArgs(loadArgs))
	}

	if ns != "" {
		opts = append(opts, WithNamespace(ns))
	}

	x, err := s.load.Load(name, opts...)
	return x, err
}

func (c *ConfigLoad) parseProto(p *exprcore.Prototype) (*ConfigRepo, error) {
	var cr ConfigRepo

	str, err := lang.StringValue(p.Attr("github"))
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); !ok {
			return nil, fmt.Errorf("error decoding 'github': %w", err)
		}
	}

	cr.Github = str

	str, err = lang.StringValue(p.Attr("path"))
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); !ok {
			return nil, fmt.Errorf("error decoding 'github': %w", err)
		}
	}

	cr.Path = str

	return &cr, nil
}
