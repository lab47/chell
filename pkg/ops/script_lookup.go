package ops

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"
)

const Extension = ".chell"

var (
	ErrNotFound = errors.New("entry not found")
)

type ScriptLookup struct {
	client httpDo

	Path []string
}

type ScriptData interface {
	Script() []byte
	Asset(name string) ([]byte, error)
}

type dirScriptData struct {
	data []byte

	dir string
}

func (s *dirScriptData) Script() []byte {
	return s.data
}

func (s *dirScriptData) Asset(name string) ([]byte, error) {
	return ioutil.ReadFile(filepath.Join(s.dir, name))
}

func (s *ScriptLookup) loadDir(dir, name string) (ScriptData, error) {
	var short string

	if len(name) > 2 {
		short = name[:2]
	} else {
		short = name
	}

	possibles := []struct {
		path, dir string
	}{
		{
			path: filepath.Join(dir, name+Extension),
			dir:  dir,
		},
		{
			path: filepath.Join(dir, "packages", name+Extension),
			dir:  filepath.Join(dir, "packages"),
		},
		{
			path: filepath.Join(dir, "packages", name, name+Extension),
			dir:  filepath.Join(dir, "packages", name),
		},
		{
			path: filepath.Join(dir, "packages", short, name+Extension),
			dir:  filepath.Join(dir, "packages", short),
		},
		{
			path: filepath.Join(dir, "packages", short, name, name+Extension),
			dir:  filepath.Join(dir, "packages", short, name),
		},
	}

	for _, x := range possibles {
		data, err := ioutil.ReadFile(x.path)
		if err == nil {
			return &dirScriptData{data: data, dir: dir}, nil
		}
	}

	return nil, ErrNotFound
}

type ghScriptData struct {
	client httpDo

	data []byte

	base string
}

func (s *ghScriptData) Script() []byte {
	return s.data
}

func (s *ghScriptData) Asset(name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s", s.base, name)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("script not available: %d", resp.StatusCode)
	}

	var content struct {
		Content []byte `json:"content"`
	}

	err = json.NewDecoder(resp.Body).Decode(&content)
	if err != nil {
		return nil, err
	}

	return content.Content, nil
}

func (s *ScriptLookup) loadGithub(client httpDo, repo, name string) (ScriptData, error) {
	slash := strings.IndexByte(repo, '/')

	if slash == -1 {
		return nil, nil
	}

	host := repo[:slash]

	if host == "github.com" {
		host = "api.github.com"
	}

	ghname := repo[slash+1:]

	var short string

	if len(name) > 2 {
		short = name[:2]
	} else {
		short = name
	}

	possibles := []struct {
		path, dir string
	}{
		{
			path: name + Extension,
		},
		{
			path: filepath.Join("packages", name+Extension),
			dir:  filepath.Join("packages"),
		},
		{
			path: filepath.Join("packages", name, name+Extension),
			dir:  filepath.Join("packages", name),
		},
		{
			path: filepath.Join("packages", short, name+Extension),
			dir:  filepath.Join("packages", short),
		},
		{
			path: filepath.Join("packages", short, name, name+Extension),
			dir:  filepath.Join("packages", short, name),
		},
	}

	var lastError error

	for _, x := range possibles {
		url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", ghname, x.path)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			lastError = fmt.Errorf("script not available: %d", resp.StatusCode)
			continue
		}

		var content struct {
			Content string `json:"content"`
		}

		err = json.NewDecoder(resp.Body).Decode(&content)
		if err != nil {
			lastError = err
			continue
		}

		data, err := base64.StdEncoding.DecodeString(content.Content)
		if err != nil {
			lastError = err
			continue
		}

		dir := x.dir
		if x.dir != "" {
			dir = "/" + dir
		}

		return &ghScriptData{
			data:   data,
			client: client,
			base:   fmt.Sprintf("https://api.github.com/repos/%s/contents%s", ghname, dir),
		}, nil
	}

	return nil, lastError
}

func (s *ScriptLookup) Load(name string) (ScriptData, error) {
	for _, p := range s.Path {
		r, err := s.loadGeneric(p, name)
		if err != nil {
			if err == ErrNotFound {
				continue
			}

			return nil, err
		}

		return r, nil
	}

	return nil, ErrNotFound
}

func (s *ScriptLookup) loadGeneric(p, name string) (ScriptData, error) {
	switch {
	case strings.HasPrefix(p, "./"):
		r, err := s.loadDir(p, name)
		if err == nil {
			return r, nil
		}
	case strings.HasPrefix(p, "/"):
		r, err := s.loadDir(p, name)
		if err == nil {
			return r, nil
		}
	case strings.HasPrefix(p, "github.com/"):
		r, err := s.loadGithub(s.client, p, name)
		if err == nil {
			return r, nil
		}
	}

	return nil, ErrNotFound
}

func (s *ScriptLookup) loadVanity(client httpDo, repo, name string) (ScriptData, error) {
	req, err := http.NewRequest("GET", "https://"+repo+"?chell-get=1", nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vanity returned error: %d", resp.StatusCode)
	}

	defer resp.Body.Close()

	imports, err := parseMetaImports(resp.Body)
	if err != nil {
		return nil, err
	}

	for _, i := range imports {
		if i.Prefix == repo {
			return s.loadGeneric(i.RepoRoot, name)
		}
	}

	return nil, fmt.Errorf("no import location")
}
