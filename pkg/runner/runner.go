package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-getter"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/archive"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/hashdetect"
	"github.com/lab47/chell/pkg/loader"
	"github.com/lab47/chell/pkg/metadata"
	"github.com/lab47/exprcore/exprcore"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
)

type Installer struct {
	storeDir string
	cacheDir string
	carDir   string

	signerId string
	signer   config.EDSigner

	carInfos map[string]*archive.CarInfo

	createdCars []string
}

func NewInstaller(rootDir string, signerId string, signer config.EDSigner) (*Installer, error) {
	storeDir := filepath.Join(rootDir, "store")
	err := os.MkdirAll(storeDir, 0755)
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(rootDir, "cache")
	err = os.MkdirAll(cacheDir, 0755)
	if err != nil {
		return nil, err
	}

	carDir := filepath.Join(rootDir, "archive")
	err = os.MkdirAll(carDir, 0755)
	if err != nil {
		return nil, err
	}

	return &Installer{
		storeDir: storeDir,
		cacheDir: cacheDir,
		carDir:   carDir,
		signerId: signerId,
		signer:   signer,
		carInfos: make(map[string]*archive.CarInfo),
	}, nil
}

func (i *Installer) CreatedCars() []string {
	return i.createdCars
}

func hashString(str string) string {
	h, _ := blake2b.New256(nil)
	h.Write([]byte(str))

	return base58.Encode(h.Sum(nil))
}

type InstallInfo = metadata.InstallInfo

func (i *Installer) CheckStore(name string) (bool, error) {
	targetDir := filepath.Join(i.storeDir, name)

	fi, err := os.Stat(targetDir)
	if err != nil {
		return false, nil
	}

	return fi.IsDir(), nil
}

func (i *Installer) carIsLocal(name string) bool {
	_, err := os.Stat(filepath.Join(i.carDir, name) + ".car")
	return err == nil
}

func (i *Installer) installCar(ctx context.Context, name string) (*metadata.InstallInfo, error) {
	ok, err := i.CheckStore(name)
	if err != nil {
		return nil, err
	}

	if ok {
		return nil, nil
	}

	return i.InstallFromLocalCAR(ctx, name)
}

func (i *Installer) MakeAvailable(ctx context.Context, s *loader.Script, force bool) error {
	name, err := s.StoreName()
	if err != nil {
		return err
	}

	if !force {
		ok, err := i.CheckStore(name)
		if err != nil {
			return err
		}

		if ok {
			return nil
		}
	}

	/*
		if i.carIsLocal(name) {
			ii, err := i.InstallFromLocalCAR(ctx, name)
			if err != nil {
				return err
			}

			spew.Dump(ii)

			return nil

			// for _, dep := range ii.Dependencies {
			// }
		}
	*/

	deps, err := s.Dependencies()
	if err != nil {
		return err
	}

	for _, dep := range deps {
		err = i.MakeAvailable(ctx, dep, false)
		if err != nil {
			return err
		}
	}

	_, err = i.Install(ctx, s)
	return err
}

func (i *Installer) carURL(s *loader.Script) (string, string, error) {
	return "", "", nil
}

func (i *Installer) InstallFromRemoteCAR(ctx context.Context, s *loader.Script) (*InstallInfo, error) {
	url, name, err := i.carURL(s)
	if err != nil {
		return nil, err
	}

	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	h, _ := blake2b.New256(nil)

	ci, err := archive.UnarchiveToDir(io.TeeReader(res.Body, h), filepath.Join(i.storeDir, name))
	if err != nil {
		return nil, err
	}

	spew.Dump(ci)

	info := &InstallInfo{
		Name: name,
		// Dependencies: ci.Dependencies,
		CarSize: res.ContentLength,
		CarHash: base58.Encode(h.Sum(nil)),
	}

	iff, err := os.Create(filepath.Join(i.storeDir, name+".json"))
	if err != nil {
		return nil, err
	}

	ienc := json.NewEncoder(iff)
	ienc.SetIndent("", "  ")

	err = ienc.Encode(info)
	if err != nil {
		return nil, err
	}

	spew.Dump(info)

	return info, nil
}

func (i *Installer) InstallFromLocalCAR(ctx context.Context, name string) (*InstallInfo, error) {
	path := filepath.Join(i.carDir, name+".car")

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	spew.Dump(path)

	defer f.Close()

	h, _ := blake2b.New256(nil)

	ci, err := archive.UnarchiveToDir(io.TeeReader(f, h), filepath.Join(i.storeDir, name))
	if err != nil {
		return nil, err
	}

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	spew.Dump(ci)

	info := &InstallInfo{
		Name: name,
		// Dependencies: ci.Dependencies,
		CarSize: fi.Size(),
		CarHash: base58.Encode(h.Sum(nil)),
	}

	iff, err := os.Create(filepath.Join(i.storeDir, name+".json"))
	if err != nil {
		return nil, err
	}

	ienc := json.NewEncoder(iff)
	ienc.SetIndent("", "  ")

	err = ienc.Encode(info)
	if err != nil {
		return nil, err
	}

	spew.Dump(info)

	return info, nil
}

func (i *Installer) Install(ctx context.Context, s *loader.Script) (*InstallInfo, error) {
	name, err := s.StoreName()
	if err != nil {
		return nil, err
	}

	targetDir := filepath.Join(i.storeDir, name)

	os.Mkdir(targetDir, 0755)

	buildDir, err := filepath.Abs(targetDir + ".build")
	if err != nil {
		return nil, err
	}

	os.Mkdir(buildDir, 0755)

	defer os.RemoveAll(buildDir)

	infoInputs := map[string]string{}

	autoChdir := "-"

	err = s.EachInput(ctx, func(name, path, algo string, hash []byte) error {
		if autoChdir == "-" {
			autoChdir = name
		} else {
			autoChdir = ""
		}

		tmp := filepath.Join(i.cacheDir, hashString(path))

		sign := "<-"
		_, err := os.Stat(tmp)
		if err == nil {
			sign = "* "
		} else {
			f, err := os.Create(tmp)
			if err != nil {
				return err
			}
			defer f.Close()

			resp, err := http.Get(path)
			if err != nil {
				return err
			}

			defer resp.Body.Close()

			h, err := hashdetect.Hasher(algo)
			if err != nil {
				return err
			}

			io.Copy(io.MultiWriter(f, h), resp.Body)

			actualHash := h.Sum(nil)

			if !bytes.Equal(hash, actualHash) {
				os.Remove(tmp)
				return fmt.Errorf("hash mismatch: %s", path)
			}
		}

		fmt.Printf("%s %s %s\n", sign, base58.Encode(hash), path)

		infoInputs[path] = base58.Encode(hash)

		archive := ""
		matchingLen := 0
		for k := range getter.Decompressors {
			if strings.HasSuffix(path, "."+k) && len(k) > matchingLen {
				archive = k
				matchingLen = len(k)
			}
		}

		target := filepath.Join(buildDir, name)

		if _, err := os.Stat(target); err == nil {
			return nil
		}

		dec, ok := getter.Decompressors[archive]
		if !ok {
			return fmt.Errorf("unknown archive type: %s", path)
		}

		return dec.Decompress(target, tmp, true, 0)
	})
	if err != nil {
		return nil, err
	}

	if autoChdir == "-" {
		autoChdir = ""
	}

	if autoChdir != "" {
		buildDir = filepath.Join(buildDir, autoChdir)

		sf, err := ioutil.ReadDir(buildDir)
		if err != nil {
			return nil, err
		}

		if len(sf) == 1 && sf[0].IsDir() {
			buildDir = filepath.Join(buildDir, sf[0].Name())
		}
	}

	/*
		filepath.Walk(buildDir, func(path string, info os.FileInfo, err error) error {
			rel, _ := filepath.Rel(buildDir, path)
			fmt.Printf("$ %s\n", rel)
			return nil
		})
	*/

	err = i.callInstall(s, targetDir, buildDir)
	if err != nil {
		os.RemoveAll(targetDir)
		return nil, err
	}

	var cinfo archive.CarInfo
	cinfo.ID = name
	cinfo.Repo = s.RepoId()

	cinfo.Name, cinfo.Version, err = s.NameAndVersion()
	if err != nil {
		return nil, err
	}

	cinfo.Inputs = infoInputs
	cinfo.Signer = i.signerId

	ar, err := archive.NewArchiver(i.storeDir, &cinfo, i.signer)
	if err != nil {
		return nil, err
	}

	carPath := filepath.Join(i.carDir, name) + ".car"

	f, err := os.Create(carPath)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	sig, err := s.Signature()
	if err != nil {
		return nil, err
	}

	carHash, err := ar.ArchiveFromPath(f, targetDir, sig)
	if err != nil {
		return nil, err
	}

	fi, err := os.Stat(carPath)
	if err != nil {
		return nil, err
	}

	carInfoPath := filepath.Join(i.carDir, name) + ".car-info.json"

	cf, err := os.Create(carInfoPath)
	if err != nil {
		return nil, err
	}

	defer cf.Close()

	enc := json.NewEncoder(cf)
	enc.SetIndent("", "  ")

	err = enc.Encode(&cinfo)
	if err != nil {
		return nil, err
	}

	i.carInfos[name] = &cinfo

	ideps, err := i.convertDependencies(cinfo.Dependencies)
	if err != nil {
		return nil, err
	}

	info := &InstallInfo{
		Name:         name,
		Dependencies: ideps,
		CarSize:      fi.Size(),
		CarHash:      base58.Encode(carHash),
	}

	iff, err := os.Create(filepath.Join(i.storeDir, name+".json"))
	if err != nil {
		return nil, err
	}

	ienc := json.NewEncoder(iff)
	ienc.SetIndent("", "  ")

	err = ienc.Encode(info)
	if err != nil {
		return nil, err
	}

	i.createdCars = append(i.createdCars, carPath, carInfoPath)

	spew.Dump(cinfo)
	spew.Dump(info)

	return info, nil
}

func (i *Installer) convertDependencies(carDeps []*archive.CarDependency) ([]metadata.InstallDepedency, error) {
	var ideps []metadata.InstallDepedency

	for _, d := range carDeps {
		ci, ok := i.carInfos[d.ID]
		if !ok {
			return nil, fmt.Errorf("missing car info: %s", d)
		}

		d.Repo = ci.Repo
		d.Signer = ci.Signer

		ideps = append(ideps, metadata.InstallDepedency{
			Id:      d.ID,
			Name:    ci.Name,
			Version: ci.Version,
			Repo:    ci.Repo,
		})
	}

	return ideps, nil
}

func (i *Installer) gatherDependencies(s *loader.Script, seen map[*loader.Script]struct{}) ([]*loader.Script, error) {
	deps, err := s.Dependencies()
	if err != nil {
		return nil, err
	}

	var scripts []*loader.Script

	for _, dep := range deps {
		if _, ok := seen[dep]; ok {
			continue
		}

		seen[dep] = struct{}{}

		scripts = append(scripts, dep)

		sub, err := i.gatherDependencies(dep, seen)
		if err != nil {
			return nil, err
		}

		scripts = append(scripts, sub...)
	}

	return scripts, nil
}

func (i *Installer) callInstall(s *loader.Script, installDir, buildDir string) error {
	fmt.Printf("performing install...\n")
	var rc RunCtx

	log := hclog.New(&hclog.LoggerOptions{
		Name:  "runctx",
		Level: hclog.Trace,
	})

	path := []string{}

	deps, err := i.gatherDependencies(s, map[*loader.Script]struct{}{})
	if err != nil {
		return err
	}

	for _, dep := range deps {
		name, err := dep.StoreName()
		if err != nil {
			return err
		}

		path = append(path, filepath.Join(i.storeDir, name, "bin"))
	}

	path = append(path, "/bin", "/usr/bin")

	rc.L = log
	rc.attrs = RunCtxFunctions
	rc.buildDir = buildDir
	rc.extraEnv = []string{"HOME=/nonexistant", "PATH=" + strings.Join(path, ":")}

	args := exprcore.Tuple{&rc}

	var thread exprcore.Thread

	thread.Shell = rc.runShell

	for _, dep := range deps {
		hook, err := dep.Hook()
		if err != nil {
			return err
		}

		if hook == nil {
			continue
		}

		dname, err := dep.StoreName()
		if err != nil {
			return err
		}

		rc.installDir = filepath.Join(i.storeDir, dname)

		_, err = exprcore.Call(&thread, hook, args, nil)
		if err != nil {
			return err
		}
	}

	rc.installDir = installDir

	fn, err := s.Install()
	if err != nil {
		return err
	}

	_, err = exprcore.Call(&thread, fn, args, nil)
	return err
}
