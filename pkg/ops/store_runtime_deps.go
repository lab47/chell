package ops

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/lab47/chell/pkg/data"
	"github.com/mr-tron/base58"
)

type StoreRuntimeDeps struct {
	storePath string
	pub       ed25519.PublicKey
	priv      ed25519.PrivateKey

	knownPackages map[string]knownPackage
}

func (s *StoreRuntimeDeps) Pack(ctx context.Context, pkg *ScriptPackage) error {
	if s.knownPackages == nil {
		s.knownPackages = make(map[string]knownPackage)
	}

	var cp CarPack
	cp.PrivateKey = s.priv
	cp.PublicKey = s.pub
	cp.DepRootDir = s.storePath
	cp.MapDependencies = s.mapDep

	var cinfo data.CarInfo

	id := pkg.ID()

	cinfo.ID = id
	cinfo.Name = pkg.cs.Name
	cinfo.Version = pkg.cs.Version
	cinfo.Repo = pkg.Repo()
	cinfo.Signer = base58.Encode(s.pub)
	cinfo.Constraints = pkg.Constraints()

	dir := filepath.Join(s.storePath, id)

	err := cp.Pack(&cinfo, dir, ioutil.Discard)
	if err != nil {
		return err
	}

	ci, err := os.Create(filepath.Join(s.storePath, id+".car-info.json"))
	if err != nil {
		return err
	}

	defer ci.Close()

	enc := json.NewEncoder(ci)
	enc.SetIndent("", "  ")

	err = enc.Encode(&cinfo)
	if err != nil {
		return err
	}

	s.knownPackages[id] = knownPackage{
		name:   pkg.cs.Name,
		repo:   pkg.Repo(),
		signer: cinfo.Signer,
	}

	return nil
}

func (s *StoreRuntimeDeps) mapDep(hash string) (string, string, string) {
	for k, kp := range s.knownPackages {
		if strings.HasPrefix(k, hash) {
			return k, kp.repo, kp.signer
		}
	}

	files, err := ioutil.ReadDir(s.storePath)
	if err != nil {
		return hash, "", ""
	}

	for _, fi := range files {
		if strings.HasPrefix(fi.Name(), hash) {
			ci, err := os.Open(filepath.Join(s.storePath, fi.Name(), ".car-info.json"))
			if err != nil {
				return fi.Name(), "", ""
			}

			defer ci.Close()

			var cinfo data.CarInfo

			err = json.NewDecoder(ci).Decode(&cinfo)
			if err != nil {
				return fi.Name(), "", ""
			}

			s.knownPackages[cinfo.ID] = knownPackage{
				name:   cinfo.Name,
				repo:   cinfo.Repo,
				signer: cinfo.Signer,
			}

			return cinfo.ID, cinfo.Repo, cinfo.Signer
		}
	}

	return hash, "", ""
}
