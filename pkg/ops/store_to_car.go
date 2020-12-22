package ops

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/lab47/chell/pkg/data"
	"github.com/mr-tron/base58"
)

type knownPackage struct {
	name, repo, signer string
}

type StoreToCar struct {
	storePath  string
	outputPath string
	pub        ed25519.PublicKey
	priv       ed25519.PrivateKey

	knownPackages map[string]knownPackage
}

func (s *StoreToCar) Pack(ctx context.Context, pkg *ScriptPackage) error {
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

	f, err := os.Create(filepath.Join(s.outputPath, id+".car"))
	if err != nil {
		return err
	}

	err = cp.Pack(&cinfo, dir, f)
	if err != nil {
		return err
	}

	of, err := os.Create(filepath.Join(s.outputPath, id+".car-info.json"))
	if err != nil {
		return err
	}

	defer of.Close()

	ci, err := os.Create(filepath.Join(s.storePath, id, ".car-info.json"))
	if err != nil {
		return err
	}

	defer ci.Close()

	enc := json.NewEncoder(io.MultiWriter(of, ci))
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

func (s *StoreToCar) mapDep(hash string) (string, string, string) {
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
