package builder

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"debug/macho"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-hclog"
	archiver "github.com/mholt/archiver/v3"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
)

type Spec struct {
	StoreName    string
	Name         string
	Version      string
	Source       string
	Dependencies []string
	Steps        []string
	Test         []string
}

type Input struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type Manifest struct {
	Inputs []*Input `json:"inputs"`
	Hash   string   `json:"hash"`
}

type Env struct {
	BuildDir string
	StoreDir string
}

const ManifestName = ".chell-manifest.json"

func (s *Spec) Build(ctx context.Context, L hclog.Logger, env *Env, build func(string, string) ([]byte, error)) (string, error) {
	var manifest Manifest

	buildDir := filepath.Join(env.BuildDir, "build")

	if _, err := os.Stat(buildDir); err == nil {
		L.Trace("reusing existing build dir")
	} else {
		os.MkdirAll(buildDir, 0755)
	}

	var tpath string

	if s.Source == "" {
		tpath = buildDir
	} else {
		source := filepath.Base(s.Source)

		spath := filepath.Join(env.BuildDir, source)

		var sourceInput *Input

		if _, err := os.Stat(spath); err == nil {
			L.Trace("reusing existing source", "source", source)
			f, err := os.Open(spath)
			if err != nil {
				return "", err
			}

			h, _ := blake2b.New(16, nil)

			io.Copy(h, f)

			sourceInput = &Input{
				Name: s.Source,
				Hash: base58.Encode(h.Sum(nil)),
			}
		} else {
			L.Info("downloading source", "url", s.Source)

			resp, err := http.Get(s.Source)
			if err != nil {
				return "", err
			}

			defer resp.Body.Close()

			f, err := os.Create(spath)
			if err != nil {
				return "", err
			}

			h, _ := blake2b.New(16, nil)
			_, err = io.Copy(f, io.TeeReader(resp.Body, h))
			if err != nil {
				return "", err
			}

			sourceInput = &Input{
				Name: s.Source,
				Hash: base58.Encode(h.Sum(nil)),
			}
		}

		manifest.Inputs = append(manifest.Inputs, sourceInput)

		L.Info("source procured", "hash", sourceInput.Hash)

		L.Trace("unpacking source", "dir", buildDir)

		f, err := archiver.ByExtension(spath)
		if err != nil {
			return "", err
		}

		ua, ok := f.(archiver.Unarchiver)
		if !ok {
			return "", fmt.Errorf("unknown source compression format")
		}

		err = ua.Unarchive(spath, buildDir)
		if err != nil {
			if !strings.Contains(err.Error(), "file already exists") {
				return "", err
			}
		}

		files, err := ioutil.ReadDir(buildDir)
		if err != nil {
			return "", err
		}

		for _, file := range files {
			if file.IsDir() {
				tpath = filepath.Join(buildDir, file.Name())
				break
			}
		}
	}

	if tpath == "" {
		return "", fmt.Errorf("no directory found after unarchiving")
	}

	storeName := s.StoreName

	manifest.Hash = s.StoreName

	installDir, err := filepath.Abs(filepath.Join(env.StoreDir, storeName))
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(installDir); err == nil {
		L.Trace("detected existing build, reusing")
	} else {
		os.MkdirAll(installDir, 0755)

		if build != nil {
			L.Trace("executing build func")

			hash, err := build(tpath, installDir)
			if err != nil {
				os.RemoveAll(installDir)
				return "", err
			}
			manifest.Inputs = append(manifest.Inputs, &Input{
				Name: "raw:build-steps",
				Hash: base58.Encode(hash),
			})

		} else {
			L.Trace("executing build steps", "count", len(s.Steps))

			bh, _ := blake2b.New(16, nil)

			for _, str := range s.Steps {
				fmt.Fprintln(bh, str)

				cmd := exec.CommandContext(ctx, "sh", "-c", str)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				cmd.Env = append(os.Environ(), "prefix="+installDir)
				cmd.Dir = tpath

				err = cmd.Run()
				if err != nil {
					return "", err
				}
			}

			manifest.Inputs = append(manifest.Inputs, &Input{
				Name: "raw:build-steps",
				Hash: base58.Encode(bh.Sum(nil)),
			})
		}
	}

	man, err := os.Create(filepath.Join(installDir, ManifestName))
	if err != nil {
		return "", err
	}

	defer man.Close()

	err = json.NewEncoder(man).Encode(&manifest)
	if err != nil {
		return "", err
	}

	man.Close()

	// Since we built fine, nuke the build dir.
	os.RemoveAll(buildDir)

	return storeName, nil
}

func (s *Spec) BoxUp(ctx context.Context, L hclog.Logger, env *Env, root, dest string) error {
	L.Trace("boxing up install dir", "dest", dest)

	of, err := os.Create(dest)
	if err != nil {
		return err
	}

	defer of.Close()

	gw := gzip.NewWriter(of)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		L.Trace("considering path", "path", path)

		if path == root {
			return nil
		}

		switch fi.Mode() & os.ModeType {
		case os.ModeSymlink:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}

			hdr, err := tar.FileInfoHeader(fi, link)
			if err != nil {
				return err
			}
			hdr.Name = path[len(root)+1:]

			err = tw.WriteHeader(hdr)
			if err != nil {
				return err
			}

			L.Trace("add entry", "path", hdr.Name)

			return nil
		case os.ModeDir:
			hdr, err := tar.FileInfoHeader(fi, "")
			if err != nil {
				return err
			}
			hdr.Name = path[len(root)+1:]

			err = tw.WriteHeader(hdr)
			if err != nil {
				return err
			}

			L.Trace("add entry", "path", hdr.Name)

			return nil
		case 0: // regular
			// ok
		default:
			// skip everything else
			return nil
		}

		mofile, err := macho.Open(path)
		if err == nil {
			type replaceLib struct {
				orig, name, lib string
			}

			var replaceLibs []*replaceLib

			for _, load := range mofile.Loads {
				if dylib, ok := load.(*macho.Dylib); ok {
					if strings.HasPrefix(dylib.Name, root) {
						parts := strings.Split(dylib.Name, "/")

						newPath := filepath.Join("@@CHELL_STORE@@", parts[len(parts)-1])

						L.Debug("pre-patching library", "file", path, "orig", dylib.Name, "new", newPath)

						replaceLibs = append(replaceLibs, &replaceLib{
							orig: dylib.Name,
							name: newPath,
						})
					} else if strings.HasPrefix(dylib.Name, env.StoreDir) {
						newPath := filepath.Join("@@CHELL_STORE@@", dylib.Name[len(env.StoreDir)+1:])

						L.Debug("pre-patching library", "file", path, "orig", dylib.Name, "new", newPath)

						replaceLibs = append(replaceLibs, &replaceLib{
							orig: dylib.Name,
							name: newPath,
						})
					} else {
						fi, err := os.Lstat(dylib.Name)
						if err != nil {
							continue

						}

						if fi.Mode()&os.ModeType != os.ModeSymlink {
							continue
						}

						target, err := os.Readlink(dylib.Name)
						if err != nil {
							continue
						}

						if strings.HasPrefix(target, env.StoreDir) {
							newPath := filepath.Join("@@CHELL_STORE@@", target[len(env.StoreDir)+1:])

							L.Debug("pre-patching library", "file", path, "orig", target, "new", newPath)

							replaceLibs = append(replaceLibs, &replaceLib{
								orig: dylib.Name,
								name: newPath,
							})
						}
					}
				}
			}

			if len(replaceLibs) > 0 {
				L.Debug("patching mach-o dependent libraries", "path", path)
			}

			perm := fi.Mode().Perm()

			err = os.Chmod(path, perm|0200)
			if err != nil {
				return err
			}

			cmd := exec.Command("install_name_tool", "-id", filepath.Base(path), path)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			err = cmd.Run()
			if err != nil {
				return err
			}

			for _, lib := range replaceLibs {
				cmd := exec.Command("install_name_tool", "-change", lib.orig, lib.name, path)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr

				err = cmd.Run()
				if err != nil {
					return err
				}
			}

			os.Chmod(path, perm)
		}

		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}

		hdr.Name = path[len(root)+1:]

		tw.WriteHeader(hdr)

		f, err := os.Open(path)
		if err != nil {
			return err
		}

		defer f.Close()

		io.Copy(tw, f)

		L.Trace("add entry", "path", hdr.Name)

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
