package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Resolver struct {
	StorePath string
}

type ResolveDB struct {
	Mappings map[string]string `json:"mappings"`
}

func (r *Resolver) loadDB() (*ResolveDB, error) {
	f, err := os.Open(filepath.Join(r.StorePath, "resolve.json"))
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var db ResolveDB

	err = json.NewDecoder(f).Decode(&db)
	if err != nil {
		return nil, err
	}

	return &db, nil
}

func (r *Resolver) Resolve(name string) (string, error) {
	db, err := r.loadDB()
	if err != nil {
		return "", err
	}

	sp, ok := db.Mappings[name]
	if !ok {
		return "", nil
	}

	return sp, nil
}

func (r *Resolver) AddResolution(name, target string) error {
	db, err := r.loadDB()
	if err != nil {
		db = &ResolveDB{
			Mappings: make(map[string]string),
		}
	}

	db.Mappings[name] = target

	f, err := os.Create(filepath.Join(r.StorePath, "resolve.json"))
	if err != nil {
		return err
	}

	defer f.Close()

	return json.NewEncoder(f).Encode(db)
}
