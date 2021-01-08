package ops

import (
	"fmt"
	"io"

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
}

var repotype = exprcore.FromStringDict(exprcore.Root, nil)

func (c *ConfigLoad) Load(r io.Reader) (*Config, error) {
	var cfg Config

	repoobj := exprcore.FromStringDict(repotype, nil)

	vars := exprcore.StringDict{
		"repo": repoobj,
	}

	_, prog, err := exprcore.SourceProgram("config.chell", r, vars.Has)
	if err != nil {
		return nil, err
	}

	var thread exprcore.Thread

	top, _, err := prog.Init(&thread, vars)
	if err != nil {
		return nil, err
	}

	cfg.Repos = make(map[string]*ConfigRepo)

	for k, v := range top {
		pv, ok := v.(*exprcore.Prototype)
		if !ok {
			return nil, fmt.Errorf("values must be created via repo: %s was a %T", k, v)
		}

		cr, err := c.parseProto(pv)
		if err != nil {
			return nil, err
		}

		cfg.Repos[k] = cr
	}

	return &cfg, nil
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
